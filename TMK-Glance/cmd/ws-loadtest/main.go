package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

type config struct {
	baseURL       string
	email         string
	connections   int
	createWorkers int
	ramp          time.Duration
	hold          time.Duration
	pingInterval  time.Duration
	audioInterval time.Duration
}

type apiEnvelope[T any] struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    T      `json:"data"`
}

type loginData struct {
	AccessToken string `json:"access_token"`
}

type sessionData struct {
	ID string `json:"id"`
}

type liveConnection struct {
	conn          *websocket.Conn
	alive         atomic.Bool
	done          chan struct{}
	framesSent    atomic.Int64
	writeFailures atomic.Int64
}

type report struct {
	Target                 string         `json:"target"`
	RequestedConnections   int            `json:"requested_connections"`
	SessionCreateSucceeded int            `json:"session_create_succeeded"`
	SessionCreateFailed    int            `json:"session_create_failed"`
	HandshakeSucceeded     int            `json:"handshake_succeeded"`
	HandshakeFailed        int            `json:"handshake_failed"`
	StableConnections      int            `json:"stable_connections"`
	DisconnectedDuringHold int            `json:"disconnected_during_hold"`
	HandshakeP50MS         int64          `json:"handshake_p50_ms"`
	HandshakeP95MS         int64          `json:"handshake_p95_ms"`
	HandshakeP99MS         int64          `json:"handshake_p99_ms"`
	RampSeconds            float64        `json:"ramp_seconds"`
	HoldSeconds            float64        `json:"hold_seconds"`
	ElapsedSeconds         float64        `json:"elapsed_seconds"`
	Errors                 map[string]int `json:"errors,omitempty"`
	CleanupDeleted         int            `json:"cleanup_deleted"`
	AudioFramesSent        int64          `json:"audio_frames_sent"`
	AudioWriteFailures     int64          `json:"audio_write_failures"`
}

func main() {
	cfg := parseFlags()
	password := os.Getenv("TMK_LOADTEST_PASSWORD")
	if strings.TrimSpace(password) == "" {
		fatal(errors.New("TMK_LOADTEST_PASSWORD is required"))
	}
	started := time.Now()
	client := &http.Client{Timeout: 15 * time.Second}
	token, err := login(client, cfg, password)
	if err != nil {
		fatal(err)
	}

	sessionIDs, createErrors := createSessions(client, cfg, token)
	result := report{
		Target: cfg.baseURL, RequestedConnections: cfg.connections,
		SessionCreateSucceeded: len(sessionIDs), SessionCreateFailed: cfg.connections - len(sessionIDs),
		RampSeconds: cfg.ramp.Seconds(), HoldSeconds: cfg.hold.Seconds(), Errors: createErrors,
	}
	connections, latencies, dialErrors := openConnections(cfg, token, sessionIDs)
	mergeErrors(result.Errors, dialErrors)
	result.HandshakeSucceeded = len(connections)
	result.HandshakeFailed = len(sessionIDs) - len(connections)
	result.HandshakeP50MS = percentile(latencies, 0.50)
	result.HandshakeP95MS = percentile(latencies, 0.95)
	result.HandshakeP99MS = percentile(latencies, 0.99)

	time.Sleep(cfg.hold)
	for _, connection := range connections {
		if connection.alive.Load() {
			result.StableConnections++
		}
		_ = connection.conn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "load test complete"), time.Now().Add(time.Second))
		_ = connection.conn.Close()
	}
	for _, connection := range connections {
		select {
		case <-connection.done:
		case <-time.After(time.Second):
		}
		result.AudioFramesSent += connection.framesSent.Load()
		result.AudioWriteFailures += connection.writeFailures.Load()
	}
	result.DisconnectedDuringHold = result.HandshakeSucceeded - result.StableConnections
	result.CleanupDeleted = cleanupSessions(client, cfg, token, sessionIDs, result.Errors)
	result.ElapsedSeconds = time.Since(started).Seconds()

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fatal(err)
	}
}

func parseFlags() config {
	var cfg config
	flag.StringVar(&cfg.baseURL, "base-url", "", "HTTP base URL, including environment prefix")
	flag.StringVar(&cfg.email, "email", "", "load-test account email")
	flag.IntVar(&cfg.connections, "connections", 100, "number of WebSocket connections")
	flag.IntVar(&cfg.createWorkers, "create-workers", 20, "concurrent session creation workers")
	flag.DurationVar(&cfg.ramp, "ramp", 10*time.Second, "connection ramp duration")
	flag.DurationVar(&cfg.hold, "hold", 60*time.Second, "stable connection hold duration")
	flag.DurationVar(&cfg.pingInterval, "ping-interval", 15*time.Second, "WebSocket control ping interval")
	flag.DurationVar(&cfg.audioInterval, "audio-interval", 0, "send 3200-byte PCM frames at this interval; zero keeps connections idle")
	flag.Parse()
	cfg.baseURL = strings.TrimRight(strings.TrimSpace(cfg.baseURL), "/")
	if cfg.baseURL == "" || cfg.email == "" || cfg.connections < 1 || cfg.createWorkers < 1 || cfg.ramp < 0 || cfg.hold < 0 {
		flag.Usage()
		os.Exit(2)
	}
	return cfg
}

func login(client *http.Client, cfg config, password string) (string, error) {
	var response apiEnvelope[loginData]
	if err := requestJSON(client, http.MethodPost, cfg.baseURL+"/api/v1/auth/login", "",
		map[string]string{"email": cfg.email, "password": password}, &response); err != nil {
		return "", fmt.Errorf("login: %w", err)
	}
	if response.Data.AccessToken == "" {
		return "", errors.New("login response did not include an access token")
	}
	return response.Data.AccessToken, nil
}

func createSessions(client *http.Client, cfg config, token string) ([]string, map[string]int) {
	type outcome struct {
		id  string
		err error
	}
	jobs := make(chan struct{})
	results := make(chan outcome, cfg.connections)
	var wg sync.WaitGroup
	workers := min(cfg.createWorkers, cfg.connections)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range jobs {
				var response apiEnvelope[sessionData]
				err := requestJSON(client, http.MethodPost, cfg.baseURL+"/api/v1/sessions", token,
					map[string]string{"source_lang": "zh", "target_lang": "en", "input_type": "load_test"}, &response)
				results <- outcome{id: response.Data.ID, err: err}
			}
		}()
	}
	go func() {
		for range cfg.connections {
			jobs <- struct{}{}
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	ids := make([]string, 0, cfg.connections)
	errorsByKind := make(map[string]int)
	for result := range results {
		if result.err != nil || result.id == "" {
			errorsByKind[errorKind(result.err)]++
			continue
		}
		ids = append(ids, result.id)
	}
	return ids, errorsByKind
}

func openConnections(cfg config, token string, sessionIDs []string) ([]*liveConnection, []time.Duration, map[string]int) {
	type outcome struct {
		connection *liveConnection
		latency    time.Duration
		err        error
	}
	results := make(chan outcome, len(sessionIDs))
	interval := time.Duration(0)
	if len(sessionIDs) > 0 {
		interval = cfg.ramp / time.Duration(len(sessionIDs))
	}
	for index, sessionID := range sessionIDs {
		if index > 0 && interval > 0 {
			time.Sleep(interval)
		}
		go func(id string) {
			started := time.Now()
			connection, _, err := websocket.DefaultDialer.Dial(websocketURL(cfg, id), http.Header{
				"Authorization": []string{"Bearer " + token},
				"User-Agent":    []string{"tmk-ws-loadtest/1"},
			})
			latency := time.Since(started)
			if err != nil {
				results <- outcome{latency: latency, err: err}
				return
			}
			live := &liveConnection{conn: connection, done: make(chan struct{})}
			live.alive.Store(true)
			if cfg.audioInterval > 0 {
				if err := connection.WriteJSON(map[string]string{"type": "start"}); err != nil {
					_ = connection.Close()
					results <- outcome{latency: latency, err: err}
					return
				}
			}
			go maintainConnection(live, cfg.pingInterval, cfg.audioInterval)
			results <- outcome{connection: live, latency: latency}
		}(sessionID)
	}

	connections := make([]*liveConnection, 0, len(sessionIDs))
	latencies := make([]time.Duration, 0, len(sessionIDs))
	errorsByKind := make(map[string]int)
	for range sessionIDs {
		result := <-results
		if result.err != nil {
			errorsByKind[errorKind(result.err)]++
			continue
		}
		connections = append(connections, result.connection)
		latencies = append(latencies, result.latency)
	}
	return connections, latencies, errorsByKind
}

func maintainConnection(connection *liveConnection, pingInterval, audioInterval time.Duration) {
	writerDone := make(chan struct{})
	go maintainWrites(connection, pingInterval, audioInterval, writerDone)
	for {
		if _, _, err := connection.conn.ReadMessage(); err != nil {
			break
		}
	}
	connection.alive.Store(false)
	close(connection.done)
	<-writerDone
}

func maintainWrites(connection *liveConnection, pingInterval, audioInterval time.Duration, done chan<- struct{}) {
	defer close(done)
	var ping, audio <-chan time.Time
	var pingTicker, audioTicker *time.Ticker
	if pingInterval > 0 {
		pingTicker = time.NewTicker(pingInterval)
		defer pingTicker.Stop()
		ping = pingTicker.C
	}
	if audioInterval > 0 {
		audioTicker = time.NewTicker(audioInterval)
		defer audioTicker.Stop()
		audio = audioTicker.C
	}
	frame := make([]byte, 3200)
	for {
		select {
		case <-ping:
			if err := connection.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
				return
			}
		case <-audio:
			if err := connection.conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
				connection.writeFailures.Add(1)
				return
			}
			connection.framesSent.Add(1)
		case <-connection.done:
			return
		}
	}
}

func cleanupSessions(client *http.Client, cfg config, token string, ids []string, errorsByKind map[string]int) int {
	deleted := 0
	for start := 0; start < len(ids); start += 100 {
		end := min(start+100, len(ids))
		var response apiEnvelope[struct {
			Deleted int `json:"deleted"`
		}]
		err := requestJSON(client, http.MethodPost, cfg.baseURL+"/api/v1/history/delete", token,
			map[string]any{"ids": ids[start:end]}, &response)
		if err != nil {
			errorsByKind["cleanup_"+errorKind(err)]++
			continue
		}
		deleted += response.Data.Deleted
	}
	return deleted
}

func websocketURL(cfg config, sessionID string) string {
	parsed, err := url.Parse(cfg.baseURL)
	if err != nil {
		panic(err)
	}
	if parsed.Scheme == "https" {
		parsed.Scheme = "wss"
	} else {
		parsed.Scheme = "ws"
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/api/v1/interpret"
	query := parsed.Query()
	query.Set("session_id", sessionID)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func requestJSON(client *http.Client, method, target, token string, payload, destination any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "tmk-ws-loadtest/1")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return fmt.Errorf("http_%d: %s", response.StatusCode, strings.TrimSpace(string(message)))
	}
	return json.NewDecoder(response.Body).Decode(destination)
}

func percentile(values []time.Duration, quantile float64) int64 {
	if len(values) == 0 {
		return 0
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	index := int(float64(len(values)-1) * quantile)
	return values[index].Milliseconds()
}

func errorKind(err error) string {
	if err == nil {
		return "unknown"
	}
	message := err.Error()
	for _, marker := range []string{"http_429", "http_500", "http_502", "http_503", "timeout", "connection reset", "connection refused", "bad handshake"} {
		if strings.Contains(strings.ToLower(message), marker) {
			return strings.ReplaceAll(marker, " ", "_")
		}
	}
	return "other"
}

func mergeErrors(destination, source map[string]int) {
	for key, value := range source {
		destination[key] += value
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
