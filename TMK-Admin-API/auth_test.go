package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSignedClaimsAndRBAC(t *testing.T) {
	secret := "test-secret"
	claims := Claims{Subject: "admin", Email: "admin@example.com", Role: "admin", ExpiresAt: time.Now().Add(time.Minute).Unix()}
	token := signClaims(claims, secret)
	parsed, ok := parseToken(token, secret)
	if !ok || parsed.Subject != "admin" {
		t.Fatalf("parsed=%+v ok=%v", parsed, ok)
	}
	app := &App{cfg: Config{ServiceSecret: secret}}
	handler := app.requireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-User-ID") != "admin" {
			t.Fatal("missing user context")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestExpiredTokenRejected(t *testing.T) {
	token := signClaims(Claims{Subject: "admin", Role: "admin", ExpiresAt: time.Now().Add(-time.Minute).Unix()}, "secret")
	if _, ok := parseToken(token, "secret"); ok {
		t.Fatal("expired token accepted")
	}
}
