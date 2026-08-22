package observability

import (
	"database/sql"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"
	"unicode/utf8"
	"tmk-glance/internal/buildinfo"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	registry              *prometheus.Registry
	httpRequests          *prometheus.CounterVec
	httpDuration          *prometheus.HistogramVec
	httpInFlight          prometheus.Gauge
	websocketConnections  prometheus.Gauge
	websocketAudio        *prometheus.CounterVec
	asrRequests           *prometheus.CounterVec
	asrDuration           *prometheus.HistogramVec
	translations          *prometheus.CounterVec
	modelTokens           *prometheus.CounterVec
	translationDuration   *prometheus.HistogramVec
	evaluationJobs        *prometheus.CounterVec
	evaluationDuration    *prometheus.HistogramVec
	evaluationJobDuration *prometheus.HistogramVec
	evaluationQueueWait   prometheus.Histogram
	evaluationTransitions *prometheus.CounterVec
	dbOpenConnections     prometheus.Gauge
	dbInUseConnections    prometheus.Gauge
	dbWaitTotal           prometheus.Counter
	lastDBWait            atomic.Int64
	evaluationQueued      prometheus.Gauge
	evaluationRunning     prometheus.Gauge
	storageBytes          prometheus.Gauge
	storageFreeBytes      prometheus.Gauge
	applicationReady      prometheus.Gauge
	build                 *prometheus.GaugeVec
}

func NewMetrics() *Metrics {
	m := &Metrics{
		registry:              prometheus.NewRegistry(),
		httpRequests:          prometheus.NewCounterVec(prometheus.CounterOpts{Name: "tmk_http_requests_total", Help: "HTTP requests by method, normalized route and status."}, []string{"method", "route", "status"}),
		httpDuration:          prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "tmk_http_request_duration_seconds", Help: "HTTP request duration by method and normalized route.", Buckets: prometheus.DefBuckets}, []string{"method", "route"}),
		httpInFlight:          prometheus.NewGauge(prometheus.GaugeOpts{Name: "tmk_http_requests_in_flight", Help: "HTTP requests currently being served."}),
		websocketConnections:  prometheus.NewGauge(prometheus.GaugeOpts{Name: "tmk_websocket_connections", Help: "Active interpretation WebSocket connections."}),
		websocketAudio:        prometheus.NewCounterVec(prometheus.CounterOpts{Name: "tmk_websocket_audio_chunks_total", Help: "WebSocket audio chunks by processing result."}, []string{"result"}),
		asrRequests:           prometheus.NewCounterVec(prometheus.CounterOpts{Name: "tmk_asr_requests_total", Help: "ASR stream attempts by mode and outcome."}, []string{"mode", "outcome"}),
		asrDuration:           prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "tmk_asr_request_duration_seconds", Help: "ASR stream duration by mode and outcome.", Buckets: []float64{.1, .5, 1, 2, 5, 10, 30, 60, 180, 600}}, []string{"mode", "outcome"}),
		translations:          prometheus.NewCounterVec(prometheus.CounterOpts{Name: "tmk_translation_requests_total", Help: "Translation attempts by mode and outcome."}, []string{"mode", "outcome"}),
		modelTokens:           prometheus.NewCounterVec(prometheus.CounterOpts{Name: "tmk_model_tokens_total", Help: "Estimated model tokens by provider, operation, and direction."}, []string{"provider", "operation", "direction"}),
		translationDuration:   prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "tmk_translation_duration_seconds", Help: "Translation duration by mode and outcome.", Buckets: []float64{.05, .1, .25, .5, 1, 2, 5, 10}}, []string{"mode", "outcome"}),
		evaluationJobs:        prometheus.NewCounterVec(prometheus.CounterOpts{Name: "tmk_evaluation_jobs_total", Help: "Evaluation jobs completed by outcome."}, []string{"outcome"}),
		evaluationDuration:    prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "tmk_evaluation_item_duration_seconds", Help: "Evaluation item duration by outcome.", Buckets: []float64{1, 5, 10, 30, 60, 180, 600}}, []string{"outcome"}),
		evaluationJobDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "tmk_evaluation_task_execution_duration_seconds", Help: "Evaluation task attempt execution duration by outcome.", Buckets: []float64{1, 5, 10, 30, 60, 180, 600, 1800, 3600}}, []string{"outcome"}),
		evaluationQueueWait:   prometheus.NewHistogram(prometheus.HistogramOpts{Name: "tmk_evaluation_task_queue_wait_seconds", Help: "Time evaluation tasks wait before an execution attempt.", Buckets: []float64{.1, .5, 1, 5, 10, 30, 60, 300, 900, 3600}}),
		evaluationTransitions: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "tmk_evaluation_task_transitions_total", Help: "Reliable evaluation task transitions."}, []string{"transition"}),
		dbOpenConnections:     prometheus.NewGauge(prometheus.GaugeOpts{Name: "tmk_db_open_connections", Help: "Open database connections."}),
		dbInUseConnections:    prometheus.NewGauge(prometheus.GaugeOpts{Name: "tmk_db_in_use_connections", Help: "Database connections currently in use."}),
		dbWaitTotal:           prometheus.NewCounter(prometheus.CounterOpts{Name: "tmk_db_connection_wait_total", Help: "Cumulative database connection waits."}),
		evaluationQueued:      prometheus.NewGauge(prometheus.GaugeOpts{Name: "tmk_evaluation_jobs_queued", Help: "Evaluation jobs waiting to run."}),
		evaluationRunning:     prometheus.NewGauge(prometheus.GaugeOpts{Name: "tmk_evaluation_jobs_running", Help: "Evaluation jobs currently running."}),
		storageBytes:          prometheus.NewGauge(prometheus.GaugeOpts{Name: "tmk_object_storage_bytes", Help: "Bytes referenced by ready storage objects."}),
		storageFreeBytes:      prometheus.NewGauge(prometheus.GaugeOpts{Name: "tmk_object_storage_free_bytes", Help: "Free bytes on the object storage filesystem."}),
		applicationReady:      prometheus.NewGauge(prometheus.GaugeOpts{Name: "tmk_application_ready", Help: "Whether core application dependencies are ready (1 or 0)."}),
		build:                 prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "tmk_build_info", Help: "Build version and commit information."}, []string{"version", "commit"}),
	}
	m.registry.MustRegister(m.httpRequests, m.httpDuration, m.httpInFlight, m.websocketConnections,
		m.websocketAudio, m.asrRequests, m.asrDuration, m.translations, m.translationDuration, m.modelTokens,
		m.evaluationJobs, m.evaluationDuration, m.evaluationJobDuration, m.evaluationQueueWait, m.evaluationTransitions,
		m.dbOpenConnections, m.dbInUseConnections, m.dbWaitTotal, m.evaluationQueued, m.evaluationRunning,
		m.storageBytes, m.storageFreeBytes, m.applicationReady, m.build, prometheus.NewGoCollector(), prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
	m.build.WithLabelValues(buildinfo.Version, buildinfo.Commit).Set(1)
	return m
}

// EstimateTokens is a provider-neutral fallback until an upstream usage field is available.
func EstimateTokens(value string) float64 {
	runes := utf8.RuneCountInString(value)
	if runes == 0 {
		return 0
	}
	return float64((runes + 3) / 4)
}

func (m *Metrics) ModelTokens(provider, operation, direction, value string) {
	if tokens := EstimateTokens(value); tokens > 0 {
		m.modelTokens.WithLabelValues(provider, operation, direction).Add(tokens)
	}
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}
func (m *Metrics) SetReady(ready bool) {
	if ready {
		m.applicationReady.Set(1)
	} else {
		m.applicationReady.Set(0)
	}
}
func (m *Metrics) BeginHTTP() { m.httpInFlight.Inc() }
func (m *Metrics) EndHTTP(method, route string, status int, elapsed time.Duration) {
	m.httpInFlight.Dec()
	m.httpRequests.WithLabelValues(method, route, strconv.Itoa(status)).Inc()
	m.httpDuration.WithLabelValues(method, route).Observe(elapsed.Seconds())
}
func (m *Metrics) WebSocketOpened()         { m.websocketConnections.Inc() }
func (m *Metrics) WebSocketClosed()         { m.websocketConnections.Dec() }
func (m *Metrics) AudioChunk(result string) { m.websocketAudio.WithLabelValues(result).Inc() }
func (m *Metrics) ASR(mode, outcome string, elapsed time.Duration) {
	m.asrRequests.WithLabelValues(mode, outcome).Inc()
	m.asrDuration.WithLabelValues(mode, outcome).Observe(elapsed.Seconds())
}
func (m *Metrics) Translation(mode, outcome string, elapsed time.Duration) {
	m.translations.WithLabelValues(mode, outcome).Inc()
	m.translationDuration.WithLabelValues(mode, outcome).Observe(elapsed.Seconds())
}
func (m *Metrics) EvaluationItem(outcome string, elapsed time.Duration) {
	m.evaluationDuration.WithLabelValues(outcome).Observe(elapsed.Seconds())
}
func (m *Metrics) EvaluationJob(outcome string, elapsed time.Duration) {
	m.evaluationJobs.WithLabelValues(outcome).Inc()
	m.evaluationJobDuration.WithLabelValues(outcome).Observe(elapsed.Seconds())
}
func (m *Metrics) EvaluationQueueWait(elapsed time.Duration) {
	if elapsed > 0 {
		m.evaluationQueueWait.Observe(elapsed.Seconds())
	}
}
func (m *Metrics) EvaluationTransition(transition string, count int) {
	if count > 0 {
		m.evaluationTransitions.WithLabelValues(transition).Add(float64(count))
	}
}
func (m *Metrics) SetRuntime(database sql.DBStats, queued, running int64, storageBytes int64, freeBytes uint64) {
	m.dbOpenConnections.Set(float64(database.OpenConnections))
	m.dbInUseConnections.Set(float64(database.InUse))
	previous := m.lastDBWait.Swap(database.WaitCount)
	if database.WaitCount > previous {
		m.dbWaitTotal.Add(float64(database.WaitCount - previous))
	}
	m.evaluationQueued.Set(float64(queued))
	m.evaluationRunning.Set(float64(running))
	m.storageBytes.Set(float64(storageBytes))
	m.storageFreeBytes.Set(float64(freeBytes))
}
