package observability

import (
	"database/sql"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMetricsExposeCoreSeries(t *testing.T) {
	metrics := NewMetrics()
	metrics.BeginHTTP()
	metrics.EndHTTP("GET", "/api/health/live", 200, 12*time.Millisecond)
	metrics.WebSocketOpened()
	metrics.AudioChunk("accepted")
	metrics.Translation("http", "success", 20*time.Millisecond)
	metrics.EvaluationItem("succeeded", time.Second)
	metrics.EvaluationJob("succeeded")
	metrics.EvaluationTransition("retry_scheduled", 1)
	metrics.SetRuntime(sql.DBStats{}, 2, 1, 1024, 2048)

	recording := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(recording, httptest.NewRequest("GET", "/metrics", nil))
	if recording.Code != 200 {
		t.Fatalf("metrics status=%d", recording.Code)
	}
	body, err := io.ReadAll(recording.Result().Body)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"tmk_http_requests_total", "tmk_http_request_duration_seconds", "tmk_websocket_audio_chunks_total", "tmk_translation_requests_total", "tmk_evaluation_jobs_total", "tmk_evaluation_task_transitions_total", "tmk_db_open_connections", "tmk_object_storage_free_bytes", "tmk_application_ready"} {
		if !strings.Contains(string(body), name) {
			t.Fatalf("metrics output missing %s:\n%s", name, body)
		}
	}
}
