package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"tmk-glance/internal/config"

	"github.com/gin-gonic/gin"
)

func newTestApplication(t *testing.T, name string) *Application {
	t.Helper()
	cfg := &config.Config{}
	cfg.Storage.Driver = "sqlite"
	cfg.Storage.DBPath = filepath.Join(t.TempDir(), name+".db")
	cfg.ASR.Provider = "mock"
	cfg.Translator.Provider = "mock"
	cfg.Auth.BootstrapAdminEmail = "test@example.com"
	cfg.Auth.BootstrapAdminPassword = "test-password-123"
	cfg.ASR.Segmenter.MaxRunes = 40
	cfg.ASR.Segmenter.MaxDurationMS = 5000
	cfg.ASR.Segmenter.SoftCommitDelayMS = 300
	cfg.Governance.SessionRetentionDays = 180
	cfg.Governance.EvaluationRetentionDays = 365
	cfg.Governance.AuditRetentionDays = 365
	cfg.Governance.StaleDraftDays = 30
	cfg.Governance.StuckJobMinutes = 30
	app, err := NewApplication(cfg)
	if err != nil {
		t.Fatalf("new application: %v", err)
	}
	user, ok, err := app.store.GetUserByEmail("test@example.com")
	if err != nil || !ok {
		t.Fatalf("get bootstrap test user: ok=%v err=%v", ok, err)
	}
	if _, err := app.store.UpdateUser(user.ID, "Test User", user.Role, user.Status, false); err != nil {
		t.Fatalf("prepare test user: %v", err)
	}
	t.Cleanup(func() { _ = app.Close() })
	return app
}

func requestJSON(router http.Handler, method, path string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	return response
}

func authorizedRequestJSON(t *testing.T, app *Application, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	user, ok, err := app.store.GetUserByEmail("test@example.com")
	if err != nil || !ok {
		t.Fatalf("get test user: ok=%v err=%v", ok, err)
	}
	pair, err := app.issueTokenPair(user)
	if err != nil {
		t.Fatalf("issue test token: %v", err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	response := httptest.NewRecorder()
	app.Router().ServeHTTP(response, req)
	return response
}

func TestRouterSessionAndHistoryLifecycle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := newTestApplication(t, "lifecycle")
	created := authorizedRequestJSON(t, app, http.MethodPost, "/api/v1/sessions", []byte(`{"source_lang":"zh","target_lang":"en","input_type":"system_audio"}`))
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	var payload struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &payload); err != nil || payload.Data.ID == "" {
		t.Fatalf("decode created session: id=%q err=%v", payload.Data.ID, err)
	}

	got := authorizedRequestJSON(t, app, http.MethodGet, "/api/v1/sessions/"+payload.Data.ID, nil)
	if got.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", got.Code, got.Body.String())
	}
	history := authorizedRequestJSON(t, app, http.MethodGet, "/api/v1/history?limit=20", nil)
	if history.Code != http.StatusOK || !bytes.Contains(history.Body.Bytes(), []byte(payload.Data.ID)) {
		t.Fatalf("history status=%d body=%s", history.Code, history.Body.String())
	}
	translated := authorizedRequestJSON(t, app, http.MethodPost, "/api/v1/translate", []byte(`{"text":"hello","source_lang":"en","target_lang":"zh"}`))
	if translated.Code != http.StatusOK {
		t.Fatalf("translate status=%d body=%s", translated.Code, translated.Body.String())
	}
}

func TestHealthEndpointsDistinguishLivenessAndReadiness(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := newTestApplication(t, "health")
	router := app.Router()
	live := requestJSON(router, http.MethodGet, "/api/health/live", nil)
	if live.Code != http.StatusOK || live.Header().Get(requestIDHeader) == "" || !bytes.Contains(live.Body.Bytes(), []byte(`"status":"alive"`)) {
		t.Fatalf("live status=%d body=%s", live.Code, live.Body.String())
	}
	request := httptest.NewRequest(http.MethodGet, "/api/health/live", nil)
	request.Header.Set(requestIDHeader, "test-request-id-123")
	withID := httptest.NewRecorder()
	router.ServeHTTP(withID, request)
	if withID.Header().Get(requestIDHeader) != "test-request-id-123" {
		t.Fatalf("request id response=%q", withID.Header().Get(requestIDHeader))
	}
	ready := requestJSON(router, http.MethodGet, "/api/health/ready", nil)
	if ready.Code != http.StatusOK || !bytes.Contains(ready.Body.Bytes(), []byte(`"ready":true`)) {
		t.Fatalf("ready status=%d body=%s", ready.Code, ready.Body.String())
	}
	legacy := requestJSON(router, http.MethodGet, "/api/health", nil)
	if legacy.Code != http.StatusOK || !bytes.Contains(legacy.Body.Bytes(), []byte(`"ready":true`)) {
		t.Fatalf("legacy health status=%d body=%s", legacy.Code, legacy.Body.String())
	}
}

func TestApplicationsKeepStoresIsolated(t *testing.T) {
	gin.SetMode(gin.TestMode)
	first := newTestApplication(t, "first")
	second := newTestApplication(t, "second")
	created := authorizedRequestJSON(t, first, http.MethodPost, "/api/v1/sessions", []byte(`{"source_lang":"zh","target_lang":"en"}`))
	var payload struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(created.Body.Bytes(), &payload)
	response := authorizedRequestJSON(t, second, http.MethodGet, "/api/v1/sessions/"+payload.Data.ID, nil)
	if response.Code != http.StatusNotFound {
		t.Fatalf("second application leaked first store: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestRouterRejectsInvalidRequestsAndHandlesCORS(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := newTestApplication(t, "validation")
	invalid := authorizedRequestJSON(t, app, http.MethodPost, "/api/v1/sessions", []byte(`{"source_lang":"zh"}`))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid session status=%d", invalid.Code)
	}
	options := requestJSON(app.Router(), http.MethodOptions, "/api/v1/history", nil)
	if options.Code != http.StatusNoContent || options.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("CORS response status=%d headers=%v", options.Code, options.Header())
	}
}

func TestLegacyAudioDevicesRouteIsRemoved(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := newTestApplication(t, "removed-audio-devices").Router()
	response := requestJSON(router, http.MethodGet, "/api/v1/audio/devices", nil)
	if response.Code != http.StatusNotFound {
		t.Fatalf("legacy audio devices status=%d body=%s", response.Code, response.Body.String())
	}
}
