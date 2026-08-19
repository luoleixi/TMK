package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"time"
)

type GlanceClient struct {
	base              string
	client            *http.Client
	serviceID, secret string
}

func NewGlanceClient(cfg Config) *GlanceClient {
	return &GlanceClient{base: cfg.GlanceURL, client: &http.Client{Timeout: cfg.RequestTimeout}, serviceID: cfg.ServiceID, secret: cfg.ServiceSecret}
}
func (c *GlanceClient) Health(ctx context.Context) error {
	response, err := c.Do(ctx, http.MethodGet, "/internal/v1/health", nil, nil, "admin-health")
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("glance health status %s", response.Status)
	}
	return nil
}
func (c *GlanceClient) Do(ctx context.Context, method, path string, body io.Reader, headers http.Header, requestID string) (*http.Response, error) {
	var data []byte
	if body != nil {
		var err error
		data, err = io.ReadAll(io.LimitReader(body, 64<<20))
		if err != nil {
			return nil, err
		}
	}
	request, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	request.Header = headers.Clone()
	if request.Header == nil {
		request.Header = make(http.Header)
	}
	request.Header.Set("X-Request-ID", requestID)
	request.Header.Set("X-Admin-API", "tmk-admin-api")
	if c.secret != "" {
		timestamp := time.Now().UTC().Format(time.RFC3339)
		mac := hmac.New(sha256.New, []byte(c.secret))
		_, _ = mac.Write([]byte(c.serviceID + "\n" + timestamp + "\n" + method + "\n" + path))
		request.Header.Set("X-Service-Timestamp", timestamp)
		request.Header.Set("X-Service-Signature", hex.EncodeToString(mac.Sum(nil)))
	}
	return c.client.Do(request)
}
