package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type Claims struct {
	Subject   string `json:"sub"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	ExpiresAt int64  `json:"exp"`
}
type TokenPair struct {
	AccessToken      string         `json:"access_token"`
	RefreshToken     string         `json:"refresh_token"`
	TokenType        string         `json:"token_type"`
	ExpiresInSeconds int            `json:"expires_in_seconds"`
	User             map[string]any `json:"user"`
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if json.NewDecoder(r.Body).Decode(&request) != nil || !hmac.Equal([]byte(strings.ToLower(strings.TrimSpace(request.Email))), []byte(strings.ToLower(env("ADMIN_API_ADMIN_EMAIL", "admin@tmk.local")))) || !hmac.Equal([]byte(request.Password), []byte(env("ADMIN_API_ADMIN_PASSWORD", ""))) {
		write(w, http.StatusUnauthorized, r, Envelope[any]{Code: "INVALID_CREDENTIALS", Message: "invalid email or password"})
		return
	}
	claims := Claims{Subject: "bootstrap-admin", Email: request.Email, Role: "admin", ExpiresAt: time.Now().Add(15 * time.Minute).Unix()}
	access := signClaims(claims, a.cfg.ServiceSecret)
	claims.ExpiresAt = time.Now().Add(24 * time.Hour).Unix()
	refresh := signClaims(claims, a.cfg.ServiceSecret)
	a.audit.Append(AuditEvent{Action: "auth.login", ResourceType: "user", ActorUserID: claims.Subject, Result: "success", OccurredAt: time.Now().UTC(), RequestID: requestID(r)})
	write(w, http.StatusOK, r, Envelope[TokenPair]{Code: "OK", Message: "ok", Data: TokenPair{AccessToken: access, RefreshToken: refresh, TokenType: "Bearer", ExpiresInSeconds: 900, User: map[string]any{"id": claims.Subject, "email": claims.Email, "role": claims.Role, "status": "active"}}})
}
func (a *App) me(w http.ResponseWriter, r *http.Request) {
	claims, ok := authenticateRequest(r, a.cfg.ServiceSecret)
	if !ok {
		write(w, http.StatusUnauthorized, r, Envelope[any]{Code: "UNAUTHORIZED", Message: "invalid or expired token"})
		return
	}
	write(w, http.StatusOK, r, Envelope[Claims]{Code: "OK", Message: "ok", Data: claims})
}
func (a *App) refresh(w http.ResponseWriter, r *http.Request) {
	var request struct {
		RefreshToken string `json:"refresh_token"`
	}
	if json.NewDecoder(r.Body).Decode(&request) != nil {
		write(w, http.StatusBadRequest, r, Envelope[any]{Code: "INVALID_REQUEST", Message: "refresh_token is required"})
		return
	}
	claims, ok := parseToken(request.RefreshToken, a.cfg.ServiceSecret)
	if !ok {
		write(w, http.StatusUnauthorized, r, Envelope[any]{Code: "UNAUTHORIZED", Message: "invalid refresh token"})
		return
	}
	claims.ExpiresAt = time.Now().Add(15 * time.Minute).Unix()
	access := signClaims(claims, a.cfg.ServiceSecret)
	claims.ExpiresAt = time.Now().Add(24 * time.Hour).Unix()
	write(w, http.StatusOK, r, Envelope[TokenPair]{Code: "OK", Message: "ok", Data: TokenPair{AccessToken: access, RefreshToken: signClaims(claims, a.cfg.ServiceSecret), TokenType: "Bearer", ExpiresInSeconds: 900, User: map[string]any{"id": claims.Subject, "email": claims.Email, "role": claims.Role, "status": "active"}}})
}
func (a *App) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authenticateRequest(r, a.cfg.ServiceSecret)
		if !ok {
			write(w, http.StatusUnauthorized, r, Envelope[any]{Code: "UNAUTHORIZED", Message: "authentication required"})
			return
		}
		if claims.Role != "admin" {
			write(w, http.StatusForbidden, r, Envelope[any]{Code: "FORBIDDEN", Message: "administrator role required"})
			return
		}
		r.Header.Set("X-User-ID", claims.Subject)
		next.ServeHTTP(w, r)
	})
}
func authenticateRequest(r *http.Request, secret string) (Claims, bool) {
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	return parseToken(token, secret)
}
func signClaims(claims Claims, secret string) string {
	data, _ := json.Marshal(claims)
	payload := base64.RawURLEncoding.EncodeToString(data)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func parseToken(token, secret string) (Claims, bool) {
	var claims Claims
	parts := strings.Split(token, ".")
	if len(parts) != 2 || secret == "" {
		return claims, false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(parts[0]))
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(signature, mac.Sum(nil)) {
		return claims, false
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || json.Unmarshal(data, &claims) != nil || claims.ExpiresAt <= time.Now().Unix() {
		return claims, false
	}
	return claims, true
}
