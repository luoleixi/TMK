package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Config struct {
	Addr           string
	GlanceURL      string
	ServiceID      string
	ServiceSecret  string
	RequestTimeout time.Duration
}

type Envelope[T any] struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
	Data      T      `json:"data,omitempty"`
}

type Event struct {
	ID          string         `json:"event_id"`
	Type        string         `json:"event_type"`
	AggregateID string         `json:"aggregate_id"`
	OccurredAt  time.Time      `json:"occurred_at"`
	RequestID   string         `json:"request_id,omitempty"`
	Payload     map[string]any `json:"payload,omitempty"`
}

type AuditEvent struct {
	Action       string         `json:"action"`
	ResourceType string         `json:"resource_type"`
	ResourceID   string         `json:"resource_id,omitempty"`
	ActorUserID  string         `json:"actor_user_id,omitempty"`
	Result       string         `json:"result"`
	Details      map[string]any `json:"details,omitempty"`
	OccurredAt   time.Time      `json:"occurred_at"`
	RequestID    string         `json:"request_id,omitempty"`
}

type App struct {
	cfg         Config
	client      *GlanceClient
	events      *EventBus
	audit       *AuditLog
	metricsData Metrics
	users       *UserStore
}

func main() {
	cfg := loadConfig()
	users, err := NewUserStore(env("ADMIN_API_DB_DSN", ""))
	if err != nil {
		slog.Error("admin database unavailable", "error", err)
		os.Exit(1)
	}
	defer users.Close()
	if err := users.EnsureBootstrap(context.Background(), env("ADMIN_API_ADMIN_EMAIL", ""), env("ADMIN_API_ADMIN_PASSWORD", "")); err != nil {
		slog.Error("admin bootstrap failed", "error", err)
		os.Exit(1)
	}
	app := &App{cfg: cfg, client: NewGlanceClient(cfg), events: NewEventBus(), audit: NewAuditLog(env("ADMIN_API_AUDIT_LOG", "./data/admin-audit.jsonl")), users: users}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health/live", app.live)
	mux.HandleFunc("/api/health/ready", app.ready)
	mux.HandleFunc("/api/health/dependencies", app.dependencies)
	mux.HandleFunc("/api/v1/auth/login", app.login)
	mux.HandleFunc("/api/v1/auth/register", app.register)
	mux.HandleFunc("/api/v1/auth/refresh", app.refresh)
	mux.HandleFunc("/api/v1/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		write(w, http.StatusOK, r, Envelope[any]{Code: "OK", Message: "ok"})
	})
	mux.Handle("/api/v1/auth/me", app.requireAdmin(http.HandlerFunc(app.me)))
	mux.Handle("/api/v1/admin/", app.requireAdmin(http.HandlerFunc(app.adminProxy)))
	mux.HandleFunc("/internal/events", app.eventsHandler)
	mux.HandleFunc("/internal/audit", app.auditHandler)
	mux.HandleFunc("/metrics", app.metrics)
	server := &http.Server{Addr: cfg.Addr, Handler: app.requestLog(app.serviceAuth(mux)), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	slog.Info("admin api starting", "addr", cfg.Addr, "glance_url", cfg.GlanceURL, "service_id", cfg.ServiceID)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("admin api stopped", "error", err)
		os.Exit(1)
	}
}

func loadConfig() Config {
	return Config{Addr: env("ADMIN_API_ADDR", ":18180"), GlanceURL: strings.TrimRight(env("GLANCE_INTERNAL_URL", "http://127.0.0.1:18080"), "/"), ServiceID: env("ADMIN_API_SERVICE_ID", "tmk-admin-api"), ServiceSecret: env("ADMIN_API_SERVICE_SECRET", ""), RequestTimeout: 10 * time.Second}
}

func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func (a *App) live(w http.ResponseWriter, r *http.Request) {
	write(w, http.StatusOK, r, Envelope[map[string]string]{Code: "OK", Message: "ok", Data: map[string]string{"status": "ok"}})
}

func (a *App) ready(w http.ResponseWriter, r *http.Request) {
	if a.cfg.ServiceSecret == "" || env("ADMIN_API_ADMIN_PASSWORD", "") == "" {
		write(w, http.StatusServiceUnavailable, r, Envelope[any]{Code: "AUTH_NOT_CONFIGURED", Message: "admin api authentication is not configured"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), a.cfg.RequestTimeout)
	defer cancel()
	if err := a.client.Health(ctx); err != nil {
		write(w, http.StatusServiceUnavailable, r, Envelope[any]{Code: "DEPENDENCY_UNAVAILABLE", Message: "glance is unavailable"})
		return
	}
	write(w, http.StatusOK, r, Envelope[map[string]string]{Code: "OK", Message: "ok", Data: map[string]string{"status": "ready"}})
}

func (a *App) dependencies(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), a.cfg.RequestTimeout)
	defer cancel()
	status := "up"
	code := http.StatusOK
	if err := a.client.Health(ctx); err != nil {
		status = "down"
		code = http.StatusServiceUnavailable
	}
	write(w, code, r, Envelope[map[string]string]{Code: "OK", Message: "dependency status", Data: map[string]string{"glance": status}})
}

func (a *App) authProxy(w http.ResponseWriter, r *http.Request) { a.proxy(w, r, r.URL.Path) }

func (a *App) adminProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost && r.Method != http.MethodPatch && r.Method != http.MethodDelete {
		write(w, http.StatusMethodNotAllowed, r, Envelope[any]{Code: "METHOD_NOT_ALLOWED", Message: "method not allowed"})
		return
	}
	path := strings.Replace(r.URL.Path, "/api/v1/admin/", "/internal/v1/admin/", 1)
	a.proxy(w, r, path)
}

func (a *App) proxy(w http.ResponseWriter, r *http.Request, path string) {
	requestID := requestID(r)
	response, err := a.client.Do(r.Context(), r.Method, path, r.Body, r.Header, requestID)
	if err != nil {
		a.metricsData.UpstreamFailures.Add(1)
		a.audit.Append(AuditEvent{Action: "admin.proxy", ResourceType: "glance", Result: "error", RequestID: requestID, OccurredAt: time.Now().UTC(), Details: map[string]any{"error": err.Error()}})
		write(w, http.StatusBadGateway, r, Envelope[any]{Code: "GLANCE_UNAVAILABLE", Message: "business service unavailable"})
		return
	}
	if r.Method != http.MethodGet {
		result := "success"
		if response.StatusCode >= 400 {
			result = "failure"
		}
		a.audit.Append(AuditEvent{Action: "admin." + strings.ToLower(r.Method), ResourceType: "admin_api", ResourceID: r.URL.Path, ActorUserID: r.Header.Get("X-User-ID"), Result: result, RequestID: requestID, OccurredAt: time.Now().UTC(), Details: map[string]any{"status": response.StatusCode}})
	}
	defer response.Body.Close()
	copyHeaders(w.Header(), response.Header)
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, response.Body)
}

func (a *App) serviceAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/metrics") {
			next.ServeHTTP(w, r)
			return
		}
		if a.cfg.ServiceSecret != "" && !validServiceSignature(r, a.cfg.ServiceID, a.cfg.ServiceSecret) {
			write(w, http.StatusUnauthorized, r, Envelope[any]{Code: "SERVICE_UNAUTHORIZED", Message: "invalid service credentials"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func validServiceSignature(r *http.Request, serviceID, secret string) bool {
	provided := r.Header.Get("X-Service-Signature")
	timestamp := r.Header.Get("X-Service-Timestamp")
	if provided == "" || timestamp == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, timestamp)
	if err != nil || time.Since(t).Abs() > 2*time.Minute {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(serviceID + "\n" + timestamp + "\n" + r.Method + "\n" + r.URL.Path))
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(strings.ToLower(provided)), []byte(expected))
}

func (a *App) eventsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		write(w, http.StatusMethodNotAllowed, r, Envelope[any]{Code: "METHOD_NOT_ALLOWED", Message: "method not allowed"})
		return
	}
	var event Event
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		write(w, http.StatusBadRequest, r, Envelope[any]{Code: "INVALID_EVENT", Message: err.Error()})
		return
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	a.events.Publish(event)
	write(w, http.StatusAccepted, r, Envelope[Event]{Code: "ACCEPTED", Message: "event accepted", Data: event})
}

func (a *App) auditHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		write(w, http.StatusMethodNotAllowed, r, Envelope[any]{Code: "METHOD_NOT_ALLOWED", Message: "method not allowed"})
		return
	}
	var event AuditEvent
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		write(w, http.StatusBadRequest, r, Envelope[any]{Code: "INVALID_AUDIT", Message: err.Error()})
		return
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	a.audit.Append(event)
	write(w, http.StatusAccepted, r, Envelope[AuditEvent]{Code: "ACCEPTED", Message: "audit accepted", Data: event})
}

func (a *App) metrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	_, _ = fmt.Fprintln(w, "# HELP tmk_admin_api_up Admin API process availability\n# TYPE tmk_admin_api_up gauge\ntmk_admin_api_up 1")
	_, _ = fmt.Fprintf(w, "# HELP tmk_admin_api_requests_total Admin API requests\n# TYPE tmk_admin_api_requests_total counter\ntmk_admin_api_requests_total %d\n", a.metricsData.Requests.Load())
	_, _ = fmt.Fprintf(w, "# HELP tmk_admin_api_upstream_failures_total Glance request failures\n# TYPE tmk_admin_api_upstream_failures_total counter\ntmk_admin_api_upstream_failures_total %d\n", a.metricsData.UpstreamFailures.Load())
}

func write[T any](w http.ResponseWriter, status int, r *http.Request, value Envelope[T]) {
	value.RequestID = requestID(r)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func requestID(r *http.Request) string {
	if value := r.Header.Get("X-Request-ID"); value != "" {
		return value
	}
	return fmt.Sprintf("admin-%d", time.Now().UnixNano())
}
func copyHeaders(dst, src http.Header) {
	for key, values := range src {
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}
func (a *App) requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		a.metricsData.Requests.Add(1)
		next.ServeHTTP(w, r)
		slog.Info("admin api request", "method", r.Method, "path", r.URL.Path, "duration_ms", time.Since(started).Milliseconds(), "request_id", requestID(r))
	})
}

type Metrics struct {
	Requests         atomic.Uint64
	UpstreamFailures atomic.Uint64
}

type EventBus struct {
	mu     sync.RWMutex
	events []Event
}

func NewEventBus() *EventBus { return &EventBus{} }
func (b *EventBus) Publish(event Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, event)
	if len(b.events) > 1000 {
		b.events = b.events[len(b.events)-1000:]
	}
}

type AuditLog struct {
	mu     sync.RWMutex
	events []AuditEvent
	path   string
}

func NewAuditLog(path string) *AuditLog { return &AuditLog{path: path} }
func (l *AuditLog) Append(event AuditEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, event)
	if len(l.events) > 5000 {
		l.events = l.events[len(l.events)-5000:]
	}
	if l.path != "" {
		_ = os.MkdirAll(filepath.Dir(l.path), 0750)
		if file, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0640); err == nil {
			defer file.Close()
			_ = json.NewEncoder(file).Encode(event)
		}
	}
}
