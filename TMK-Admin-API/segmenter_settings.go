package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

func (a *App) segmenterSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		a.getSegmenterSettings(w, r)
		return
	}
	if r.Method == http.MethodPut {
		a.updateSegmenterSettings(w, r)
		return
	}
	w.Header().Set("Allow", "GET, PUT")
	write(w, http.StatusMethodNotAllowed, r, Envelope[any]{Code: "METHOD_NOT_ALLOWED", Message: "method not allowed"})
}

func (a *App) getSegmenterSettings(w http.ResponseWriter, r *http.Request) {
	value, err := a.users.GetSegmenterRuntimeConfig(r.Context())
	if err != nil {
		write(w, http.StatusInternalServerError, r, Envelope[any]{Code: "CONFIG_READ_FAILED", Message: "read segmenter configuration failed"})
		return
	}
	write(w, http.StatusOK, r, Envelope[SegmenterRuntimeConfig]{Code: "OK", Message: "ok", Data: value})
}

func (a *App) updateSegmenterSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled        *bool          `json:"enabled"`
		RolloutPercent *int           `json:"rollout_percent"`
		Version        string         `json:"version"`
		Config         map[string]any `json:"config"`
		ChangeReason   string         `json:"change_reason"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || req.Enabled == nil || strings.TrimSpace(req.ChangeReason) == "" {
		write(w, http.StatusBadRequest, r, Envelope[any]{Code: "INVALID_REQUEST", Message: "enabled and change_reason are required"})
		return
	}
	if req.RolloutPercent == nil {
		value, _ := a.users.GetSegmenterRuntimeConfig(r.Context())
		req.RolloutPercent = &value.RolloutPercent
		if *req.RolloutPercent == 0 {
			*req.RolloutPercent = 100
		}
	}
	if *req.RolloutPercent < 0 || *req.RolloutPercent > 100 {
		write(w, http.StatusBadRequest, r, Envelope[any]{Code: "INVALID_REQUEST", Message: "rollout_percent must be between 0 and 100"})
		return
	}
	current, err := a.users.GetSegmenterRuntimeConfig(r.Context())
	if err != nil {
		write(w, http.StatusInternalServerError, r, Envelope[any]{Code: "CONFIG_READ_FAILED", Message: "read segmenter configuration failed"})
		return
	}
	if req.Version == "" {
		req.Version = current.Version
	}
	if req.Config == nil {
		req.Config = current.Config
	}
	value := SegmenterRuntimeConfig{Enabled: *req.Enabled, RolloutPercent: *req.RolloutPercent, Version: req.Version, Config: req.Config, Revision: current.Revision + 1, Status: "pending", ChangedBy: r.Header.Get("X-User-ID"), ChangeReason: strings.TrimSpace(req.ChangeReason), CreatedAt: time.Now().UTC()}
	if err := a.users.SaveSegmenterRuntimeConfig(r.Context(), value); err != nil {
		write(w, http.StatusInternalServerError, r, Envelope[any]{Code: "CONFIG_WRITE_FAILED", Message: "save segmenter configuration failed"})
		return
	}
	body, _ := json.Marshal(map[string]any{"enabled": value.Enabled, "rollout_percent": value.RolloutPercent, "version": value.Version, "config": value.Config, "revision": value.Revision})
	response, callErr := a.client.Do(r.Context(), http.MethodPut, "/internal/v1/runtime/segmenter", bytes.NewReader(body), http.Header{"Content-Type": []string{"application/json"}}, requestID(r))
	if callErr == nil {
		defer response.Body.Close()
	}
	if callErr == nil && response.StatusCode >= 200 && response.StatusCode < 300 {
		_ = a.users.MarkSegmenterApplied(r.Context(), value.Revision)
		value.Status = "applied"
		value.AppliedAt = ptrTime(time.Now().UTC())
	} else if callErr != nil {
		value.Status = "pending"
	} else {
		value.Status = "failed"
	}
	a.audit.Append(AuditEvent{Action: "segmenter.config.update", ResourceType: "segmenter_runtime_config", ResourceID: "1", ActorUserID: value.ChangedBy, Result: value.Status, OccurredAt: time.Now().UTC(), RequestID: requestID(r), Details: map[string]any{"revision": value.Revision, "enabled": value.Enabled, "rollout_percent": value.RolloutPercent, "error": errorString(callErr)}})
	if value.Status == "failed" {
		write(w, http.StatusBadGateway, r, Envelope[SegmenterRuntimeConfig]{Code: "CONFIG_APPLY_FAILED", Message: "configuration saved but Glance rejected it", Data: value})
		return
	}
	write(w, http.StatusOK, r, Envelope[SegmenterRuntimeConfig]{Code: "OK", Message: "configuration applied", Data: value})
}

func ptrTime(value time.Time) *time.Time { return &value }
func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
