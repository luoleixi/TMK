package session

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func newHTTPTestService(server *httptest.Server) *SessionService {
	return &SessionService{
		httpClient:   server.Client(),
		apiURL:       func() string { return server.URL + "/api/v1" },
		webSocketURL: func(string, url.Values) (string, error) { return "ws://unused", nil },
		dialer:       websocket.DefaultDialer,
	}
}

func TestCreateSessionUsesInjectedBackend(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/sessions" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"id":"session-1"}}`))
	}))
	defer server.Close()

	service := newHTTPTestService(server)
	id, err := service.CreateSession("zh", "en", "system_audio")
	if err != nil || id != "session-1" {
		t.Fatalf("create session id=%q err=%v", id, err)
	}
	if service.sessionID != id {
		t.Fatalf("session state not updated: %q", service.sessionID)
	}
}

func TestHistoryContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/history":
			if r.URL.Query().Get("keyword") != "budget" || r.URL.Query().Get("offset") != "2" {
				t.Errorf("unexpected query: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"data":{"total":1,"sessions":[{"id":"s1","source_lang":"zh","target_lang":"en"}]}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/history/delete":
			var body map[string][]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if len(body["ids"]) != 2 {
				t.Errorf("unexpected batch body: %+v", body)
			}
			_, _ = w.Write([]byte(`{"data":{"deleted":2}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	service := newHTTPTestService(server)
	sessions, total, err := service.SearchHistory(2, 10, "budget", time.Now().Format(time.RFC3339), "")
	if err != nil || total != 1 || len(sessions) != 1 || sessions[0].ID != "s1" {
		t.Fatalf("search history sessions=%+v total=%d err=%v", sessions, total, err)
	}
	deleted, err := service.DeleteHistoryBatch([]string{"s1", "s2"})
	if err != nil || deleted != 2 {
		t.Fatalf("delete batch deleted=%d err=%v", deleted, err)
	}
}

func TestDisconnectedOperationsFail(t *testing.T) {
	service := NewService()
	if err := service.SendAudio([]byte{1}); err == nil {
		t.Fatal("SendAudio should fail while disconnected")
	}
	if err := service.PauseInterpret(); err == nil {
		t.Fatal("PauseInterpret should fail while disconnected")
	}
}
