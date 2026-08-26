package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type changeRequestInput struct {
	Environment string          `json:"environment"`
	Type        string          `json:"type"`
	Target      string          `json:"target"`
	OldValue    json.RawMessage `json:"old_value"`
	NewValue    json.RawMessage `json:"new_value"`
	ReleaseID   string          `json:"release_id"`
	CommitSHA   string          `json:"commit_sha"`
	Reason      string          `json:"reason"`
}

func newChangeID() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "cr_" + time.Now().UTC().Format("20060102150405.000000000")
	}
	return "cr_" + hex.EncodeToString(buf)
}

func (a *App) changeRequests(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		values, err := a.users.ListChangeRequests(r.Context(), r.URL.Query().Get("environment"), r.URL.Query().Get("status"))
		if err != nil {
			write(w, http.StatusInternalServerError, r, Envelope[any]{Code: "CHANGE_REQUEST_READ_FAILED", Message: "list change requests failed"})
			return
		}
		write(w, http.StatusOK, r, Envelope[[]ChangeRequest]{Code: "OK", Message: "ok", Data: values})
		return
	}
	if r.Method != http.MethodPost {
		write(w, http.StatusMethodNotAllowed, r, Envelope[any]{Code: "METHOD_NOT_ALLOWED", Message: "method not allowed"})
		return
	}
	var input changeRequestInput
	if json.NewDecoder(r.Body).Decode(&input) != nil || input.Environment == "" || input.Type == "" || len(input.NewValue) == 0 || strings.TrimSpace(input.Reason) == "" {
		write(w, http.StatusBadRequest, r, Envelope[any]{Code: "INVALID_REQUEST", Message: "environment, type, new_value and reason are required"})
		return
	}
	if input.Environment != "test" && input.Environment != "production" {
		write(w, http.StatusBadRequest, r, Envelope[any]{Code: "INVALID_ENVIRONMENT", Message: "environment must be test or production"})
		return
	}
	if input.Type != "runtime_config" && input.Type != "release" && input.Type != "rollback" {
		write(w, http.StatusBadRequest, r, Envelope[any]{Code: "INVALID_TYPE", Message: "unsupported change request type"})
		return
	}
	status := "pending_approval"
	if input.Environment == "test" && input.Type == "runtime_config" {
		status = "approved"
	}
	now := time.Now().UTC()
	request := ChangeRequest{ID: newChangeID(), Environment: input.Environment, Type: input.Type, Target: input.Target, OldValue: input.OldValue, NewValue: input.NewValue, ReleaseID: input.ReleaseID, CommitSHA: input.CommitSHA, Status: status, RequestedBy: r.Header.Get("X-User-ID"), Reason: strings.TrimSpace(input.Reason), CreatedAt: now, ExpiresAt: ptrTime(now.Add(24 * time.Hour))}
	if err := a.users.CreateChangeRequest(r.Context(), request); err != nil {
		write(w, http.StatusInternalServerError, r, Envelope[any]{Code: "CHANGE_REQUEST_CREATE_FAILED", Message: "create change request failed"})
		return
	}
	a.audit.Append(AuditEvent{Action: "change_request.create", ResourceType: "change_request", ResourceID: request.ID, ActorUserID: request.RequestedBy, Result: "success", RequestID: requestID(r), OccurredAt: now})
	write(w, http.StatusCreated, r, Envelope[ChangeRequest]{Code: "OK", Message: "change request created", Data: request})
}

func (a *App) changeRequestRoute(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/change-requests/"), "/"), "/")
	if len(parts) == 1 && parts[0] != "" && r.Method == http.MethodGet {
		a.getChangeRequest(w, r, parts[0])
		return
	}
	if len(parts) != 2 || r.Method != http.MethodPost {
		write(w, http.StatusMethodNotAllowed, r, Envelope[any]{Code: "METHOD_NOT_ALLOWED", Message: "method not allowed"})
		return
	}
	switch parts[1] {
	case "approve":
		a.approveChangeRequest(w, r, parts[0])
	case "reject":
		a.transitionChangeRequest(w, r, parts[0], "pending_approval", "rejected", nil)
	case "execute":
		a.executeChangeRequest(w, r, parts[0])
	case "rollback":
		a.rollbackChangeRequest(w, r, parts[0])
	default:
		write(w, http.StatusNotFound, r, Envelope[any]{Code: "NOT_FOUND", Message: "change request action not found"})
	}
}

func (a *App) getChangeRequest(w http.ResponseWriter, r *http.Request, id string) {
	value, err := a.users.GetChangeRequest(r.Context(), id)
	if err != nil {
		write(w, http.StatusNotFound, r, Envelope[any]{Code: "NOT_FOUND", Message: "change request not found"})
		return
	}
	write(w, http.StatusOK, r, Envelope[ChangeRequest]{Code: "OK", Message: "ok", Data: value})
}

func (a *App) approveChangeRequest(w http.ResponseWriter, r *http.Request, id string) {
	actor := r.Header.Get("X-User-ID")
	ok, err := a.users.ApproveChangeRequest(r.Context(), id, actor, time.Now().UTC())
	if err != nil {
		write(w, http.StatusInternalServerError, r, Envelope[any]{Code: "APPROVAL_FAILED", Message: "approve change request failed"})
		return
	}
	if !ok {
		write(w, http.StatusConflict, r, Envelope[any]{Code: "APPROVAL_REJECTED", Message: "request is expired, not pending, or approver is requester"})
		return
	}
	a.getChangeRequest(w, r, id)
}

func (a *App) executeChangeRequest(w http.ResponseWriter, r *http.Request, id string) {
	request, err := a.users.GetChangeRequest(r.Context(), id)
	if err != nil {
		write(w, http.StatusNotFound, r, Envelope[any]{Code: "NOT_FOUND", Message: "change request not found"})
		return
	}
	if request.ExpiresAt != nil && time.Now().UTC().After(*request.ExpiresAt) {
		_, _ = a.users.TransitionChangeRequest(r.Context(), id, request.Status, "expired", nil, time.Now().UTC())
		write(w, http.StatusConflict, r, Envelope[any]{Code: "CHANGE_REQUEST_EXPIRED", Message: "change request expired"})
		return
	}
	if request.Status != "approved" {
		write(w, http.StatusConflict, r, Envelope[any]{Code: "NOT_APPROVED", Message: "change request is not approved"})
		return
	}
	if request.Type == "release" || request.Type == "rollback" {
		token := newChangeID()
		result, _ := json.Marshal(map[string]any{"approval_token": token, "environment": request.Environment, "release_id": request.ReleaseID, "commit_sha": request.CommitSHA, "expires_at": request.ExpiresAt})
		_, _ = a.users.TransitionChangeRequest(r.Context(), id, "approved", "executing", result, time.Now().UTC())
		a.getChangeRequest(w, r, id)
		return
	}
	if request.Environment != "production" {
		_, _ = a.users.TransitionChangeRequest(r.Context(), id, "approved", "succeeded", request.NewValue, time.Now().UTC())
		a.getChangeRequest(w, r, id)
		return
	}
	response, callErr := a.client.DoBase(r.Context(), a.cfg.EnvironmentURLs[request.Environment], http.MethodPut, "/internal/v1/agent/settings/segmenter", bytes.NewReader(request.NewValue), http.Header{"Content-Type": []string{"application/json"}}, requestID(r))
	if callErr == nil {
		response.Body.Close()
	}
	status, result := "succeeded", request.NewValue
	if callErr != nil || response.StatusCode < 200 || response.StatusCode >= 300 {
		status, result = "failed", json.RawMessage(`{"error":"agent rejected configuration"}`)
	}
	_, _ = a.users.TransitionChangeRequest(r.Context(), id, "approved", status, result, time.Now().UTC())
	a.getChangeRequest(w, r, id)
}

func (a *App) rollbackChangeRequest(w http.ResponseWriter, r *http.Request, id string) {
	request, err := a.users.GetChangeRequest(r.Context(), id)
	if err != nil {
		write(w, http.StatusNotFound, r, Envelope[any]{Code: "NOT_FOUND", Message: "change request not found"})
		return
	}
	if request.Type == "runtime_config" && len(request.OldValue) > 0 {
		response, callErr := a.client.DoBase(r.Context(), a.cfg.EnvironmentURLs[request.Environment], http.MethodPut, "/internal/v1/agent/settings/segmenter", bytes.NewReader(request.OldValue), http.Header{"Content-Type": []string{"application/json"}}, requestID(r))
		if callErr == nil {
			response.Body.Close()
		}
		if callErr != nil || response.StatusCode < 200 || response.StatusCode >= 300 {
			write(w, http.StatusBadGateway, r, Envelope[any]{Code: "ROLLBACK_FAILED", Message: "rollback rejected by agent"})
			return
		}
	}
	_, _ = a.users.TransitionChangeRequest(r.Context(), id, request.Status, "rolled_back", request.OldValue, time.Now().UTC())
	a.getChangeRequest(w, r, id)
}

func (a *App) transitionChangeRequest(w http.ResponseWriter, r *http.Request, id, from, to string, result json.RawMessage) {
	ok, err := a.users.TransitionChangeRequest(r.Context(), id, from, to, result, time.Now().UTC())
	if err != nil {
		write(w, http.StatusInternalServerError, r, Envelope[any]{Code: "STATE_UPDATE_FAILED", Message: "change request update failed"})
		return
	}
	if !ok {
		write(w, http.StatusConflict, r, Envelope[any]{Code: "STATE_CONFLICT", Message: "change request state changed"})
		return
	}
	a.getChangeRequest(w, r, id)
}

func (a *App) authorizeChangeRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		write(w, http.StatusMethodNotAllowed, r, Envelope[any]{Code: "METHOD_NOT_ALLOWED", Message: "method not allowed"})
		return
	}
	ok, err := a.users.AuthorizeChangeRequest(r.Context(), r.URL.Query().Get("environment"), r.URL.Query().Get("release_id"), r.URL.Query().Get("commit_sha"), r.URL.Query().Get("token"))
	if err != nil {
		write(w, http.StatusInternalServerError, r, Envelope[any]{Code: "AUTHORIZATION_CHECK_FAILED", Message: "authorization check failed"})
		return
	}
	if !ok {
		write(w, http.StatusForbidden, r, Envelope[any]{Code: "DEPLOYMENT_NOT_APPROVED", Message: "no valid production approval"})
		return
	}
	write(w, http.StatusOK, r, Envelope[map[string]bool]{Code: "OK", Message: "deployment authorized", Data: map[string]bool{"authorized": true}})
}
