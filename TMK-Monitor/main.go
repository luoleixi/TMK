package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type config struct {
	Port            string
	PrometheusURL   string
	AlertmanagerURL string
	TargetHealthURL string
	Environment     string
	RequestTimeout  time.Duration
}

type server struct {
	cfg    config
	client *http.Client
}

type envelope[T any] struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    T      `json:"data,omitempty"`
}

type alert struct {
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	State       string            `json:"state"`
	Status      struct {
		State string `json:"state"`
	} `json:"status,omitempty"`
	ActiveAt string `json:"activeAt,omitempty"`
	Value    string `json:"value,omitempty"`
}

type targetStatus struct {
	URL        string `json:"url"`
	Up         bool   `json:"up"`
	StatusCode int    `json:"status_code"`
	LatencyMS  int64  `json:"latency_ms"`
	Error      string `json:"error,omitempty"`
}

type metricValue struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit,omitempty"`
	Error string  `json:"error,omitempty"`
}

type summary struct {
	GeneratedAt time.Time              `json:"generated_at"`
	Environment string                 `json:"environment"`
	Target      targetStatus           `json:"target"`
	Alerts      []alert                `json:"alerts"`
	Metrics     map[string]metricValue `json:"metrics"`
}

type prometheusResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Value []json.RawMessage `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

func main() {
	cfg := loadConfig()
	s := &server{cfg: cfg, client: &http.Client{Timeout: cfg.RequestTimeout}}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health/live", s.live)
	mux.HandleFunc("/api/health/ready", s.ready)
	mux.HandleFunc("/api/monitoring/summary", s.summary)
	mux.HandleFunc("/api/monitoring/alerts", s.alerts)
	mux.HandleFunc("/api/monitoring/query", s.query)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { writeError(w, http.StatusNotFound, "not found") })

	httpServer := &http.Server{Addr: cfg.Port, Handler: requestLog(mux), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	slog.Info("monitor service starting", "port", cfg.Port, "environment", cfg.Environment)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("monitor service stopped", "error", err)
		os.Exit(1)
	}
}

func loadConfig() config {
	return config{
		Port:            env("MONITOR_PORT", ":19090"),
		PrometheusURL:   strings.TrimRight(env("PROMETHEUS_URL", "http://127.0.0.1:9090"), "/"),
		AlertmanagerURL: strings.TrimRight(env("ALERTMANAGER_URL", "http://127.0.0.1:9093"), "/"),
		TargetHealthURL: env("TMK_HEALTH_URL", "http://127.0.0.1:18080/api/health/ready"),
		Environment:     env("MONITOR_ENVIRONMENT", "test"),
		RequestTimeout:  5 * time.Second,
	}
}

func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func (s *server) live(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, envelope[map[string]any]{Code: 0, Message: "ok", Data: map[string]any{"status": "ok"}})
}

func (s *server) ready(w http.ResponseWriter, _ *http.Request) {
	if _, err := url.ParseRequestURI(s.cfg.PrometheusURL); err != nil {
		writeError(w, http.StatusServiceUnavailable, "prometheus url is invalid")
		return
	}
	writeJSON(w, http.StatusOK, envelope[map[string]any]{Code: 0, Message: "ok", Data: map[string]any{"status": "ok", "monitoring_source": "prometheus"}})
}

func (s *server) summary(w http.ResponseWriter, r *http.Request) {
	target := s.probeTarget(r.Context())
	metrics := make(map[string]metricValue)
	scope := `{environment="` + s.cfg.Environment + `"}`
	queries := map[string]string{
		"application_ready":      `tmk_application_ready` + scope,
		"websocket_connections":  `tmk_websocket_connections` + scope,
		"audio_drop_rate":        `sum(rate(tmk_websocket_audio_chunks_total{result="dropped",environment="` + s.cfg.Environment + `"}[5m])) / clamp_min(sum(rate(tmk_websocket_audio_chunks_total` + scope + `[5m])), 0.001)`,
		"http_5xx_rate":          `sum(rate(tmk_http_requests_total{status=~"5..",environment="` + s.cfg.Environment + `"}[5m])) / clamp_min(sum(rate(tmk_http_requests_total` + scope + `[5m])), 1)`,
		"http_p95_seconds":       `histogram_quantile(0.95, sum by (le) (rate(tmk_http_request_duration_seconds_bucket` + scope + `[5m])))`,
		"asr_error_rate":         `sum(rate(tmk_asr_requests_total{outcome="error",environment="` + s.cfg.Environment + `"}[10m])) / clamp_min(sum(rate(tmk_asr_requests_total{outcome=~"success|error",environment="` + s.cfg.Environment + `"}[10m])), 0.001)`,
		"translation_error_rate": `sum(rate(tmk_translation_requests_total{outcome=~"fallback|error",environment="` + s.cfg.Environment + `"}[10m])) / clamp_min(sum(rate(tmk_translation_requests_total` + scope + `[10m])), 0.001)`,
		"evaluation_queued":      `tmk_evaluation_jobs_queued` + scope,
		"evaluation_running":     `tmk_evaluation_jobs_running` + scope,
		"database_in_use":        `tmk_db_in_use_connections` + scope,
		"storage_free_bytes":     `tmk_object_storage_free_bytes` + scope,
	}
	for name, query := range queries {
		metrics[name] = s.queryValue(r.Context(), query)
	}
	alerts := s.fetchAlerts(r.Context())
	writeJSON(w, http.StatusOK, envelope[summary]{Code: 0, Message: "ok", Data: summary{GeneratedAt: time.Now().UTC(), Environment: s.cfg.Environment, Target: target, Alerts: alerts, Metrics: metrics}})
}

func (s *server) alerts(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, envelope[[]alert]{Code: 0, Message: "ok", Data: s.fetchAlerts(r.Context())})
}

func (s *server) query(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("name"))
	allowed := map[string]string{
		"application_ready":     `tmk_application_ready{environment="` + s.cfg.Environment + `"}`,
		"websocket_connections": `tmk_websocket_connections{environment="` + s.cfg.Environment + `"}`,
		"evaluation_queued":     `tmk_evaluation_jobs_queued{environment="` + s.cfg.Environment + `"}`,
		"evaluation_running":    `tmk_evaluation_jobs_running{environment="` + s.cfg.Environment + `"}`,
		"storage_free_bytes":    `tmk_object_storage_free_bytes{environment="` + s.cfg.Environment + `"}`,
	}
	promQL, ok := allowed[query]
	if !ok {
		writeError(w, http.StatusBadRequest, "unsupported metric")
		return
	}
	writeJSON(w, http.StatusOK, envelope[metricValue]{Code: 0, Message: "ok", Data: s.queryValue(r.Context(), promQL)})
}

func (s *server) probeTarget(ctx context.Context) targetStatus {
	started := time.Now()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.cfg.TargetHealthURL, nil)
	if err != nil {
		return targetStatus{URL: s.cfg.TargetHealthURL, Error: err.Error(), LatencyMS: time.Since(started).Milliseconds()}
	}
	response, err := s.client.Do(request)
	result := targetStatus{URL: s.cfg.TargetHealthURL, LatencyMS: time.Since(started).Milliseconds()}
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer response.Body.Close()
	result.StatusCode = response.StatusCode
	result.Up = response.StatusCode >= 200 && response.StatusCode < 300
	if !result.Up {
		result.Error = response.Status
	}
	return result
}

func (s *server) fetchAlerts(ctx context.Context) []alert {
	filter := url.QueryEscape(`environment="` + s.cfg.Environment + `"`)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.cfg.AlertmanagerURL+"/api/v2/alerts?active=true&silenced=false&inhibited=false&filter="+filter, nil)
	if err != nil {
		return []alert{{Labels: map[string]string{"source": "alertmanager"}, Annotations: map[string]string{"error": err.Error()}, State: "error"}}
	}
	response, err := s.client.Do(request)
	if err != nil {
		return []alert{{Labels: map[string]string{"source": "alertmanager"}, Annotations: map[string]string{"error": err.Error()}, State: "error"}}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return []alert{{Labels: map[string]string{"source": "alertmanager"}, Annotations: map[string]string{"error": response.Status}, State: "error"}}
	}
	var values []alert
	if err := json.NewDecoder(response.Body).Decode(&values); err != nil {
		return []alert{{Labels: map[string]string{"source": "alertmanager"}, Annotations: map[string]string{"error": err.Error()}, State: "error"}}
	}
	for i := range values {
		if values[i].State == "" {
			values[i].State = values[i].Status.State
		}
	}
	return values
}

func (s *server) queryValue(ctx context.Context, promQL string) metricValue {
	requestURL := s.cfg.PrometheusURL + "/api/v1/query?query=" + url.QueryEscape(promQL)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return metricValue{Error: err.Error()}
	}
	response, err := s.client.Do(request)
	if err != nil {
		return metricValue{Error: err.Error()}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return metricValue{Error: response.Status}
	}
	var payload prometheusResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return metricValue{Error: err.Error()}
	}
	if payload.Status != "success" || len(payload.Data.Result) == 0 || len(payload.Data.Result[0].Value) < 2 {
		return metricValue{Error: "metric unavailable"}
	}
	var value string
	if err := json.Unmarshal(payload.Data.Result[0].Value[1], &value); err != nil {
		return metricValue{Error: err.Error()}
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return metricValue{Error: err.Error()}
	}
	return metricValue{Value: parsed}
}

func requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		slog.Info("monitor request", "method", r.Method, "path", r.URL.Path, "duration_ms", time.Since(started).Milliseconds())
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, envelope[any]{Code: status, Message: message})
}
