package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

type config struct {
	Port            string
	PrometheusURL   string
	AlertmanagerURL string
	TargetHealthURL string
	AdminHealthURL  string
	Environment     string
	RequestTimeout  time.Duration
	BasicUser       string
	BasicPassword   string
	WebhookToken    string
	LogPath         string
	DeploymentPath  string
	IncidentPath    string
}

type server struct {
	cfg      config
	client   *http.Client
	requests atomic.Uint64
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
	AdminTarget targetStatus           `json:"admin_target"`
	Alerts      []alert                `json:"alerts"`
	Metrics     map[string]metricValue `json:"metrics"`
	DataSources map[string]string      `json:"data_sources"`
}

type logEntry struct {
	Timestamp string `json:"timestamp"`
	Service   string `json:"service"`
	Level     string `json:"level"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
	TraceID   string `json:"trace_id,omitempty"`
}
type deploymentEntry struct {
	ReleaseID   string `json:"release_id"`
	Service     string `json:"service"`
	ChangeType  string `json:"change_type"`
	Version     string `json:"version"`
	Commit      string `json:"commit"`
	Environment string `json:"environment"`
	DeployedAt  string `json:"deployed_at"`
	Result      string `json:"result"`
}
type incident struct {
	ID        string  `json:"id"`
	StartedAt string  `json:"started_at"`
	Severity  string  `json:"severity"`
	Service   string  `json:"service"`
	Summary   string  `json:"summary"`
	RootCause string  `json:"root_cause,omitempty"`
	Alerts    []alert `json:"alerts"`
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
	mux.HandleFunc("/api/health/dependencies", s.dependencies)
	mux.HandleFunc("/metrics", s.metrics)
	mux.HandleFunc("/api/monitoring/summary", s.summary)
	mux.HandleFunc("/api/monitoring/alerts", s.alerts)
	mux.HandleFunc("/api/monitoring/alerts/webhook", s.alertWebhook)
	mux.HandleFunc("/api/monitoring/query", s.query)
	mux.HandleFunc("/api/monitoring/logs", s.logs)
	mux.HandleFunc("/api/monitoring/deployments", s.deployments)
	mux.HandleFunc("/api/monitoring/incidents", s.incidents)
	mux.HandleFunc("/emergency/", s.emergency)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { writeError(w, http.StatusNotFound, "not found") })

	httpServer := &http.Server{Addr: cfg.Port, Handler: s.requestLog(s.auth(mux)), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
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
		AdminHealthURL:  env("ADMIN_API_HEALTH_URL", "http://127.0.0.1:18180/api/health/live"),
		Environment:     env("MONITOR_ENVIRONMENT", "test"),
		RequestTimeout:  5 * time.Second,
		BasicUser:       env("MONITOR_BASIC_USER", "monitor"),
		BasicPassword:   env("MONITOR_BASIC_PASSWORD", ""),
		WebhookToken:    env("MONITOR_WEBHOOK_TOKEN", ""),
		LogPath:         env("MONITOR_LOG_PATH", "/var/log/tmk/combined.jsonl"),
		DeploymentPath:  env("MONITOR_DEPLOYMENT_PATH", "/var/lib/tmk/deployments.jsonl"),
		IncidentPath:    env("MONITOR_INCIDENT_PATH", "/var/lib/tmk-monitor/incidents.jsonl"),
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

func (s *server) ready(w http.ResponseWriter, r *http.Request) {
	components := map[string]targetStatus{
		"prometheus":   s.probe(r.Context(), s.cfg.PrometheusURL+"/-/ready"),
		"alertmanager": s.probe(r.Context(), s.cfg.AlertmanagerURL+"/-/ready"),
	}
	status := http.StatusOK
	for _, component := range components {
		if !component.Up {
			status = http.StatusServiceUnavailable
		}
	}
	writeJSON(w, status, envelope[map[string]any]{Code: statusCode(status), Message: "monitoring infrastructure status", Data: map[string]any{"status": map[bool]string{true: "ready", false: "degraded"}[status == http.StatusOK], "components": components}})
}

func (s *server) dependencies(w http.ResponseWriter, r *http.Request) {
	result := map[string]targetStatus{"glance": s.probeTarget(r.Context()), "admin_api": s.probe(r.Context(), s.cfg.AdminHealthURL)}
	status := http.StatusOK
	for _, target := range result {
		if !target.Up {
			status = http.StatusServiceUnavailable
		}
	}
	writeJSON(w, status, envelope[map[string]targetStatus]{Code: 0, Message: "dependency status", Data: result})
}

func (s *server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health/live" || r.URL.Path == "/api/health/ready" || r.URL.Path == "/metrics" || r.URL.Path == "/api/monitoring/alerts/webhook" {
			next.ServeHTTP(w, r)
			return
		}
		if s.cfg.BasicPassword == "" {
			writeError(w, http.StatusServiceUnavailable, "monitor emergency credentials are not configured")
			return
		}
		user, password, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(user), []byte(s.cfg.BasicUser)) != 1 || subtle.ConstantTimeCompare([]byte(password), []byte(s.cfg.BasicPassword)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="TMK Emergency Monitoring"`)
			writeError(w, http.StatusUnauthorized, "monitor authentication required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) metrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	_, _ = w.Write([]byte("# TYPE tmk_monitor_up gauge\ntmk_monitor_up 1\n# TYPE tmk_monitor_requests_total counter\ntmk_monitor_requests_total " + strconv.FormatUint(s.requests.Load(), 10) + "\n"))
}

func (s *server) summary(w http.ResponseWriter, r *http.Request) {
	target := s.probeTarget(r.Context())
	adminTarget := s.probe(r.Context(), s.cfg.AdminHealthURL)
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
		"model_tokens_per_second": `sum(rate(tmk_model_tokens_total` + scope + `[5m]))`,
		"evaluation_queued":      `tmk_evaluation_jobs_queued` + scope,
		"evaluation_running":     `tmk_evaluation_jobs_running` + scope,
		"database_in_use":        `tmk_db_in_use_connections` + scope,
		"storage_free_bytes":     `tmk_object_storage_free_bytes` + scope,
	}
	for name, query := range queries {
		metrics[name] = s.queryValue(r.Context(), query)
	}
	alerts := s.fetchAlerts(r.Context())
	writeJSON(w, http.StatusOK, envelope[summary]{Code: 0, Message: "ok", Data: summary{GeneratedAt: time.Now().UTC(), Environment: s.cfg.Environment, Target: target, AdminTarget: adminTarget, Alerts: alerts, Metrics: metrics, DataSources: map[string]string{"metrics": s.cfg.PrometheusURL, "alerts": s.cfg.AlertmanagerURL, "logs": s.cfg.LogPath, "deployments": s.cfg.DeploymentPath}}})
}

func (s *server) alerts(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, envelope[[]alert]{Code: 0, Message: "ok", Data: s.fetchAlerts(r.Context())})
}
func (s *server) alertWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.cfg.WebhookToken == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+s.cfg.WebhookToken)) != 1 {
		writeError(w, http.StatusUnauthorized, "invalid alert webhook credential")
		return
	}
	var payload struct {
		Status string         `json:"status"`
		Alerts []webhookAlert `json:"alerts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid alert payload")
		return
	}
	now := time.Now().UTC()
	for index, current := range payload.Alerts {
		currentAlert := alert{Labels: current.Labels, Annotations: current.Annotations, State: current.Status, ActiveAt: current.StartsAt, Value: current.Value}
		service := current.Labels["service"]
		if service == "" {
			service = current.Labels["job"]
		}
		entry := incident{
			ID:        fmt.Sprintf("%d-%d", now.UnixNano(), index),
			StartedAt: now.Format(time.RFC3339),
			Severity:  current.Labels["severity"],
			Service:   service,
			Summary:   current.Annotations["summary"],
			RootCause: current.Annotations["description"],
			Alerts:    []alert{currentAlert},
		}
		if err := appendJSONLine(s.cfg.IncidentPath, entry); err != nil {
			slog.Error("persist alert incident", "error", err)
			writeError(w, http.StatusInternalServerError, "could not persist alert incident")
			return
		}
	}
	writeJSON(w, http.StatusAccepted, envelope[map[string]any]{Code: 0, Message: "alert accepted", Data: map[string]any{"status": payload.Status, "count": len(payload.Alerts)}})
}

type webhookAlert struct {
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	Status      string            `json:"status"`
	StartsAt    string            `json:"startsAt,omitempty"`
	Value       string            `json:"value,omitempty"`
}

func appendJSONLine(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Clean(path), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640)
	if err != nil {
		return err
	}
	defer file.Close()
	return json.NewEncoder(file).Encode(value)
}

func (s *server) emergency(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/emergency/" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(emergencyHTML))
}

const emergencyHTML = `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>TMK Emergency Monitor</title><style>
*{box-sizing:border-box}body{margin:0;background:#111518;color:#e9eef1;font:14px system-ui,sans-serif}header{padding:18px 24px;border-bottom:1px solid #30383d;display:flex;justify-content:space-between;align-items:center}h1{font-size:18px;margin:0}main{padding:20px;display:grid;gap:16px}.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(240px,1fr));gap:12px}section{border:1px solid #30383d;background:#181d20;padding:14px;border-radius:6px}h2{font-size:14px;margin:0 0 12px;color:#aebbc2}.ok{color:#5bd692}.bad{color:#ff7979}.muted{color:#8e9aa0}table{width:100%;border-collapse:collapse}th,td{text-align:left;padding:8px;border-bottom:1px solid #293034;vertical-align:top}pre{white-space:pre-wrap;word-break:break-word;margin:0;max-height:320px;overflow:auto}button{border:1px solid #4c5a61;background:#222a2e;color:#fff;padding:7px 11px;border-radius:4px;cursor:pointer}@media(max-width:640px){header{padding:14px}main{padding:12px}table{font-size:12px}}
</style></head><body><header><h1>TMK 带外故障排查</h1><button onclick="load()">刷新</button></header><main><div class="grid"><section><h2>业务依赖</h2><div id="deps">加载中</div></section><section><h2>当前告警</h2><div id="alerts">加载中</div></section><section><h2>指标摘要</h2><div id="metrics">加载中</div></section></div><section><h2>故障时间线</h2><div id="incidents">加载中</div></section><section><h2>最近部署与变更</h2><div id="deployments">加载中</div></section><section><h2>关联日志</h2><pre id="logs">加载中</pre></section></main><script>
const esc=v=>String(v??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
async function get(path){const r=await fetch('../api/'+path,{credentials:'same-origin'});if(!r.ok)throw new Error(r.status+' '+r.statusText);return (await r.json()).data}
function rows(items,cols){if(!items?.length)return '<span class="muted">暂无数据</span>';return '<table><tbody>'+items.slice().reverse().map(x=>'<tr>'+cols.map(c=>'<td>'+esc(typeof c==='function'?c(x):x[c])+'</td>').join('')+'</tr>').join('')+'</tbody></table>'}
async function load(){try{const [s,i,d,l]=await Promise.all([get('monitoring/summary'),get('monitoring/incidents'),get('monitoring/deployments'),get('monitoring/logs')]);deps.innerHTML=[['Glance',s.target],['Admin API',s.admin_target]].map(([n,x])=>'<div class="'+(x.up?'ok':'bad')+'">'+n+': '+(x.up?'UP':'DOWN')+' '+esc(x.error||x.latency_ms+'ms')+'</div>').join('');alerts.innerHTML=rows(s.alerts,[x=>x.labels?.alertname||x.labels?.source,x=>x.annotations?.summary||x.annotations?.error,x=>x.state]);metrics.innerHTML=Object.entries(s.metrics||{}).map(([k,v])=>'<div>'+esc(k)+': '+esc(v.error||v.value)+'</div>').join('');incidents.innerHTML=rows(i,['started_at','severity','service','summary','root_cause']);deployments.innerHTML=rows(d,['deployed_at','environment','service','release_id','result']);logs.textContent=(l||[]).slice().reverse().map(x=>[x.timestamp,x.service,x.level,x.message].filter(Boolean).join(' | ')).join('\n')||'暂无日志'}catch(e){document.querySelectorAll('#deps,#alerts,#metrics,#incidents,#deployments,#logs').forEach(x=>x.textContent='加载失败: '+e.message)}}load();setInterval(load,30000);
</script></body></html>`

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

func (s *server) logs(w http.ResponseWriter, r *http.Request) {
	var values []logEntry
	readJSONLines(s.cfg.LogPath, 200, &values)
	writeJSON(w, http.StatusOK, envelope[[]logEntry]{Code: 0, Message: "ok", Data: values})
}
func (s *server) deployments(w http.ResponseWriter, r *http.Request) {
	var values []deploymentEntry
	readJSONLines(s.cfg.DeploymentPath, 100, &values)
	writeJSON(w, http.StatusOK, envelope[[]deploymentEntry]{Code: 0, Message: "ok", Data: values})
}
func (s *server) incidents(w http.ResponseWriter, r *http.Request) {
	var values []incident
	readJSONLines(s.cfg.IncidentPath, 100, &values)
	if len(values) == 0 {
		values = []incident{{ID: "live", StartedAt: time.Now().UTC().Format(time.RFC3339), Severity: "info", Service: "monitor", Summary: "当前监控数据由旁路系统聚合", Alerts: s.fetchAlerts(r.Context())}}
	}
	writeJSON(w, http.StatusOK, envelope[[]incident]{Code: 0, Message: "ok", Data: values})
}
func readJSONLines(path string, limit int, target any) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	switch output := target.(type) {
	case *[]logEntry:
		for _, line := range lines {
			var value logEntry
			if json.Unmarshal([]byte(line), &value) == nil && value.Message != "" {
				*output = append(*output, value)
			}
		}
	case *[]deploymentEntry:
		for _, line := range lines {
			var value deploymentEntry
			if json.Unmarshal([]byte(line), &value) == nil && value.ReleaseID != "" {
				*output = append(*output, value)
			}
		}
	case *[]incident:
		for _, line := range lines {
			var value incident
			if json.Unmarshal([]byte(line), &value) == nil && value.ID != "" {
				*output = append(*output, value)
			}
		}
	}
}

func (s *server) probeTarget(ctx context.Context) targetStatus {
	return s.probe(ctx, s.cfg.TargetHealthURL)
}

func (s *server) probe(ctx context.Context, targetURL string) targetStatus {
	started := time.Now()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return targetStatus{URL: targetURL, Error: err.Error(), LatencyMS: time.Since(started).Milliseconds()}
	}
	response, err := s.client.Do(request)
	result := targetStatus{URL: targetURL, LatencyMS: time.Since(started).Milliseconds()}
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

func (s *server) requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		s.requests.Add(1)
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

func statusCode(status int) int {
	if status >= http.StatusBadRequest {
		return status
	}
	return 0
}
