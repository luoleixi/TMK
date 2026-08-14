package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"tmk-glance/internal/config"
	"tmk-glance/internal/model"

	"github.com/gin-gonic/gin"
)

func loginTestUser(t *testing.T, app *Application, email, password string) tokenPair {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"email": email, "password": password})
	response := requestJSON(app.Router(), http.MethodPost, "/api/v1/auth/login", body)
	if response.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data tokenPair `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	return envelope.Data
}

func requestWithToken(router http.Handler, method, path string, body []byte, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	return response
}

func TestLoginRefreshLogoutLifecycle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := newTestApplication(t, "auth-lifecycle")
	pair := loginTestUser(t, app, "test@example.com", "test-password-123")

	me := requestWithToken(app.Router(), http.MethodGet, "/api/v1/auth/me", nil, pair.AccessToken)
	if me.Code != http.StatusOK || !bytes.Contains(me.Body.Bytes(), []byte("test@example.com")) {
		t.Fatalf("me status=%d body=%s", me.Code, me.Body.String())
	}

	refreshBody, _ := json.Marshal(map[string]string{"refresh_token": pair.RefreshToken})
	refreshed := requestJSON(app.Router(), http.MethodPost, "/api/v1/auth/refresh", refreshBody)
	if refreshed.Code != http.StatusOK {
		t.Fatalf("refresh status=%d body=%s", refreshed.Code, refreshed.Body.String())
	}
	var refreshedEnvelope struct {
		Data tokenPair `json:"data"`
	}
	_ = json.Unmarshal(refreshed.Body.Bytes(), &refreshedEnvelope)
	if refreshedEnvelope.Data.RefreshToken == pair.RefreshToken || refreshedEnvelope.Data.AccessToken == pair.AccessToken {
		t.Fatal("refresh did not rotate both tokens")
	}

	replay := requestJSON(app.Router(), http.MethodPost, "/api/v1/auth/refresh", refreshBody)
	if replay.Code != http.StatusUnauthorized {
		t.Fatalf("refresh replay status=%d body=%s", replay.Code, replay.Body.String())
	}

	logoutBody, _ := json.Marshal(map[string]string{"refresh_token": refreshedEnvelope.Data.RefreshToken})
	loggedOut := requestWithToken(app.Router(), http.MethodPost, "/api/v1/auth/logout", logoutBody, refreshedEnvelope.Data.AccessToken)
	if loggedOut.Code != http.StatusOK {
		t.Fatalf("logout status=%d body=%s", loggedOut.Code, loggedOut.Body.String())
	}
	afterLogout := requestWithToken(app.Router(), http.MethodGet, "/api/v1/auth/me", nil, refreshedEnvelope.Data.AccessToken)
	if afterLogout.Code != http.StatusUnauthorized {
		t.Fatalf("revoked access status=%d", afterLogout.Code)
	}
}

func TestApplicationRefusesEmptyIdentityStoreWithoutBootstrapAdmin(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.Driver = "sqlite"
	cfg.Storage.DBPath = filepath.Join(t.TempDir(), "empty.db")
	cfg.ASR.Provider = "mock"
	cfg.Translator.Provider = "mock"
	app, err := NewApplication(cfg)
	if err == nil {
		_ = app.Close()
		t.Fatal("application started without a bootstrap admin")
	}
}

func TestSessionOwnershipIsolation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := newTestApplication(t, "ownership")
	owner := loginTestUser(t, app, "test@example.com", "test-password-123")
	created := requestWithToken(app.Router(), http.MethodPost, "/api/v1/sessions",
		[]byte(`{"source_lang":"zh","target_lang":"en"}`), owner.AccessToken)
	var createdEnvelope struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(created.Body.Bytes(), &createdEnvelope)

	adminUser, ok, err := app.store.GetUserByEmail("test@example.com")
	if err != nil || !ok {
		t.Fatalf("get owner: %v", err)
	}
	adminUser.Email = "other@example.com"
	adminUser.ID = "other-user"
	adminUser.Role = model.RoleUser
	if err := app.store.CreateUser(adminUser); err != nil {
		t.Fatalf("create other user: %v", err)
	}
	other := loginTestUser(t, app, "other@example.com", "test-password-123")
	response := requestWithToken(app.Router(), http.MethodGet, "/api/v1/sessions/"+createdEnvelope.Data.ID, nil, other.AccessToken)
	if response.Code != http.StatusNotFound {
		t.Fatalf("cross-user session status=%d body=%s", response.Code, response.Body.String())
	}
	history := requestWithToken(app.Router(), http.MethodGet, "/api/v1/history", nil, other.AccessToken)
	if history.Code != http.StatusOK || bytes.Contains(history.Body.Bytes(), []byte(createdEnvelope.Data.ID)) {
		t.Fatalf("cross-user history leaked: %s", history.Body.String())
	}
}

func TestAdminRoleIsRequiredForUserManagement(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := newTestApplication(t, "admin-rbac")
	admin := loginTestUser(t, app, "test@example.com", "test-password-123")
	createBody := []byte(`{"email":"member@example.com","display_name":"Member","password":"temporary-pass-123","role":"user"}`)
	created := requestWithToken(app.Router(), http.MethodPost, "/api/v1/admin/users", createBody, admin.AccessToken)
	if created.Code != http.StatusCreated {
		t.Fatalf("admin create user status=%d body=%s", created.Code, created.Body.String())
	}
	member, ok, err := app.store.GetUserByEmail("member@example.com")
	if err != nil || !ok || !member.MustChangePassword {
		t.Fatalf("created member ok=%v must_change=%v err=%v", ok, member != nil && member.MustChangePassword, err)
	}
	if _, err := app.store.UpdateUser(member.ID, member.DisplayName, member.Role, member.Status, false); err != nil {
		t.Fatal(err)
	}
	regular := loginTestUser(t, app, "member@example.com", "temporary-pass-123")
	denied := requestWithToken(app.Router(), http.MethodGet, "/api/v1/admin/users", nil, regular.AccessToken)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("regular user admin status=%d body=%s", denied.Code, denied.Body.String())
	}
}
