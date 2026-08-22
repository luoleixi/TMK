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
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

type config struct {
	baseURL    string
	email      string
	path       string
	concurrent int
	duration   time.Duration
	warmup     time.Duration
}

type workerResult struct {
	latencies []time.Duration
	statuses  map[int]int
	errors    int
}

type report struct {
	Target       string         `json:"target"`
	Concurrency  int            `json:"concurrency"`
	Duration     float64        `json:"duration_seconds"`
	Requests     int            `json:"requests"`
	QPS          float64        `json:"qps"`
	P50MS        float64        `json:"p50_ms"`
	P95MS        float64        `json:"p95_ms"`
	P99MS        float64        `json:"p99_ms"`
	MaxMS        float64        `json:"max_ms"`
	Errors       int            `json:"errors"`
	ErrorRate    float64        `json:"error_rate"`
	StatusCounts map[string]int `json:"status_counts"`
}

func main() {
	cfg := parseFlags()
	password := os.Getenv("TMK_LOADTEST_PASSWORD")
	if password == "" {
		fatal(errors.New("TMK_LOADTEST_PASSWORD is required"))
	}
	transport := &http.Transport{
		MaxIdleConns:        cfg.concurrent * 2,
		MaxIdleConnsPerHost: cfg.concurrent * 2,
		MaxConnsPerHost:     cfg.concurrent * 2,
		IdleConnTimeout:     30 * time.Second,
	}
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second}
	token, err := login(client, cfg, password)
	if err != nil {
		fatal(err)
	}
	if cfg.warmup > 0 {
		_ = execute(client, cfg, token, cfg.warmup)
	}
	result := execute(client, cfg, token, cfg.duration)
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
	flag.StringVar(&cfg.path, "path", "/api/v1/languages", "authenticated GET path")
	flag.IntVar(&cfg.concurrent, "concurrency", 10, "concurrent request workers")
	flag.DurationVar(&cfg.duration, "duration", 30*time.Second, "measurement duration")
	flag.DurationVar(&cfg.warmup, "warmup", 2*time.Second, "warmup duration")
	flag.Parse()
	cfg.baseURL = strings.TrimRight(strings.TrimSpace(cfg.baseURL), "/")
	if cfg.baseURL == "" || cfg.email == "" || cfg.concurrent < 1 || cfg.duration <= 0 {
		flag.Usage()
		os.Exit(2)
	}
	return cfg
}

func login(client *http.Client, cfg config, password string) (string, error) {
	payload, _ := json.Marshal(map[string]string{"email": cfg.email, "password": password})
	request, err := http.NewRequest(http.MethodPost, cfg.baseURL+"/api/v1/auth/login", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("login status %d", response.StatusCode)
	}
	var envelope struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return "", err
	}
	if envelope.Data.AccessToken == "" {
		return "", errors.New("login response did not include an access token")
	}
	return envelope.Data.AccessToken, nil
}

func execute(client *http.Client, cfg config, token string, duration time.Duration) report {
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()
	results := make(chan workerResult, cfg.concurrent)
	var wg sync.WaitGroup
	started := time.Now()
	for range cfg.concurrent {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result := workerResult{statuses: make(map[int]int)}
			for ctx.Err() == nil {
				request, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.baseURL+cfg.path, nil)
				if err != nil {
					result.errors++
					continue
				}
				request.Header.Set("Authorization", "Bearer "+token)
				request.Header.Set("User-Agent", "tmk-http-loadtest/1")
				requestStarted := time.Now()
				response, err := client.Do(request)
				elapsed := time.Since(requestStarted)
				if err != nil {
					if ctx.Err() == nil {
						result.errors++
					}
					continue
				}
				_, _ = io.Copy(io.Discard, response.Body)
				_ = response.Body.Close()
				result.latencies = append(result.latencies, elapsed)
				result.statuses[response.StatusCode]++
				if response.StatusCode < 200 || response.StatusCode >= 300 {
					result.errors++
				}
			}
			results <- result
		}()
	}
	wg.Wait()
	close(results)
	elapsed := time.Since(started)
	var latencies []time.Duration
	statusCounts := make(map[string]int)
	errorsTotal := 0
	for worker := range results {
		latencies = append(latencies, worker.latencies...)
		errorsTotal += worker.errors
		for status, count := range worker.statuses {
			statusCounts[fmt.Sprint(status)] += count
		}
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	requests := len(latencies) + errorsTotal
	result := report{
		Target:       cfg.baseURL + cfg.path,
		Concurrency:  cfg.concurrent,
		Duration:     elapsed.Seconds(),
		Requests:     requests,
		Errors:       errorsTotal,
		StatusCounts: statusCounts,
	}
	if elapsed > 0 {
		result.QPS = float64(requests) / elapsed.Seconds()
	}
	if requests > 0 {
		result.ErrorRate = float64(errorsTotal) / float64(requests)
	}
	result.P50MS = percentile(latencies, .50)
	result.P95MS = percentile(latencies, .95)
	result.P99MS = percentile(latencies, .99)
	result.MaxMS = percentile(latencies, 1)
	return result
}

func percentile(values []time.Duration, quantile float64) float64 {
	if len(values) == 0 {
		return 0
	}
	index := int(float64(len(values)-1) * quantile)
	return float64(values[index].Microseconds()) / 1000
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
