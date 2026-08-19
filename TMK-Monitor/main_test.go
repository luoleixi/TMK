package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSummaryScopesMetricsAndAlertsToEnvironment(t *testing.T) {
	var queries []string
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"status":"success","data":{"resultType":"vector","result":[{"value":["0","2"]}]}}`
		switch r.URL.Path {
		case "/health":
			return response(http.StatusNoContent, ""), nil
		case "/api/v1/query":
			queries = append(queries, r.URL.Query().Get("query"))
			return response(http.StatusOK, body), nil
		case "/api/v2/alerts":
			return response(http.StatusOK, `[{"labels":{"alertname":"Example","environment":"test"},"annotations":{"summary":"ok"},"status":{"state":"firing"}}]`), nil
		default:
			return response(http.StatusNotFound, ""), nil
		}
	})

	s := &server{
		cfg: config{
			PrometheusURL:   "http://prometheus",
			AlertmanagerURL: "http://alertmanager",
			TargetHealthURL: "http://glance/health",
			Environment:     "test",
			RequestTimeout:  time.Second,
		},
		client: &http.Client{Timeout: time.Second, Transport: transport},
	}
	recorder := httptest.NewRecorder()
	s.summary(recorder, httptest.NewRequest(http.MethodGet, "/api/monitoring/summary", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("summary status = %d", recorder.Code)
	}
	var response envelope[summary]
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Data.Environment != "test" || len(response.Data.Alerts) != 1 || response.Data.Alerts[0].State != "firing" {
		t.Fatalf("unexpected summary: %+v", response.Data)
	}
	if len(queries) == 0 {
		t.Fatal("expected prometheus queries")
	}
	for _, query := range queries {
		if !strings.Contains(query, `environment="test"`) {
			t.Errorf("query is not environment-scoped: %s", query)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Status: http.StatusText(status), Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func TestQueryRejectsUnknownMetric(t *testing.T) {
	s := &server{cfg: config{Environment: "test"}}
	recorder := httptest.NewRecorder()
	s.query(recorder, httptest.NewRequest(http.MethodGet, "/api/monitoring/query?name=secret", nil))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestAlertWebhookPersistsIncidentWithIndependentToken(t *testing.T) {
	path := t.TempDir() + "/incidents.jsonl"
	s := &server{cfg: config{WebhookToken: "secret", IncidentPath: path}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/monitoring/alerts/webhook", strings.NewReader(`{"status":"firing","alerts":[{"labels":{"alertname":"Down","service":"glance","severity":"critical"},"annotations":{"summary":"Glance down"}}]}`))
	request.Header.Set("Authorization", "Bearer secret")
	s.alertWebhook(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("webhook status = %d", recorder.Code)
	}
	if data, err := os.ReadFile(path); err != nil || !strings.Contains(string(data), `"service":"glance"`) {
		t.Fatalf("incident was not persisted: %v %s", err, data)
	}
}

func TestEmergencyAuthDoesNotDependOnAdmin(t *testing.T) {
	s := &server{cfg: config{BasicUser: "monitor", BasicPassword: "password"}}
	handler := s.requestLog(s.auth(http.HandlerFunc(s.emergency)))
	request := httptest.NewRequest(http.MethodGet, "/emergency/", nil)
	request.SetBasicAuth("monitor", "password")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "带外故障排查") {
		t.Fatalf("emergency page unavailable: %d", recorder.Code)
	}
}
