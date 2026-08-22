package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type authPayload struct {
	AccessToken      string   `json:"access_token"`
	RefreshToken     string   `json:"refresh_token"`
	ExpiresInSeconds int      `json:"expires_in_seconds"`
	User             AuthUser `json:"user"`
}

func (s *SessionService) Login(email, password string) (AuthUser, error) {
	body, err := json.Marshal(map[string]string{"email": strings.TrimSpace(email), "password": password})
	if err != nil {
		return AuthUser{}, err
	}
	req, err := http.NewRequest(http.MethodPost, s.apiURL()+"/auth/login", bytes.NewReader(body))
	if err != nil {
		return AuthUser{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return AuthUser{}, fmt.Errorf("login: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return AuthUser{}, responseError(resp)
	}
	payload, err := decodeAuthPayload(resp)
	if err != nil {
		return AuthUser{}, fmt.Errorf("login response: %w", err)
	}
	s.setAuth(payload)
	return payload.User, nil
}

func (s *SessionService) Logout() error {
	s.authMu.RLock()
	refresh := s.refreshToken
	s.authMu.RUnlock()
	body, _ := json.Marshal(map[string]string{"refresh_token": refresh})
	resp, err := s.doAuthenticated(http.MethodPost, s.apiURL()+"/auth/logout", body)
	if resp != nil {
		_ = resp.Body.Close()
	}
	s.clearAuth()
	return err
}

func (s *SessionService) ChangePassword(currentPassword, newPassword string) error {
	body, err := json.Marshal(map[string]string{"current_password": currentPassword, "new_password": newPassword})
	if err != nil {
		return err
	}
	resp, err := s.doAuthenticated(http.MethodPost, s.apiURL()+"/auth/change-password", body)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err == nil {
		s.clearAuth()
	}
	return err
}

func (s *SessionService) AuthState() AuthUser {
	s.authMu.RLock()
	defer s.authMu.RUnlock()
	return s.user
}

func (s *SessionService) doAuthenticated(method, target string, body []byte) (*http.Response, error) {
	token, err := s.validAccessToken()
	if err != nil {
		return nil, err
	}
	resp, err := s.sendAuthenticated(method, target, body, token)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		_ = resp.Body.Close()
		token, err = s.refreshAccessToken(token)
		if err != nil {
			return nil, err
		}
		resp, err = s.sendAuthenticated(method, target, body, token)
		if err != nil {
			return nil, err
		}
	}
	if resp.StatusCode >= 300 {
		err := responseError(resp)
		_ = resp.Body.Close()
		return nil, err
	}
	return resp, nil
}

func (s *SessionService) sendAuthenticated(method, target string, body []byte, token string) (*http.Response, error) {
	req, err := http.NewRequest(method, target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return s.httpClient.Do(req)
}

func (s *SessionService) validAccessToken() (string, error) {
	s.authMu.RLock()
	token, expiry := s.accessToken, s.accessExpiry
	s.authMu.RUnlock()
	if token == "" {
		return "", fmt.Errorf("authentication required")
	}
	if time.Until(expiry) > 30*time.Second {
		return token, nil
	}
	return s.refreshAccessToken("")
}

func (s *SessionService) refreshAccessToken(failedAccessToken string) (string, error) {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()

	s.authMu.RLock()
	currentAccess, refresh, expiry := s.accessToken, s.refreshToken, s.accessExpiry
	s.authMu.RUnlock()
	if failedAccessToken != "" && currentAccess != failedAccessToken && time.Until(expiry) > 30*time.Second {
		return currentAccess, nil
	}
	if failedAccessToken == "" && time.Until(expiry) > 30*time.Second {
		return currentAccess, nil
	}
	if refresh == "" {
		s.clearAuth()
		return "", fmt.Errorf("authentication expired; sign in again")
	}
	body, _ := json.Marshal(map[string]string{"refresh_token": refresh})
	req, err := http.NewRequest(http.MethodPost, s.apiURL()+"/auth/refresh", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("refresh authentication: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		s.clearAuth()
		return "", fmt.Errorf("authentication expired; sign in again")
	}
	payload, err := decodeAuthPayload(resp)
	if err != nil {
		return "", err
	}
	s.setAuth(payload)
	return payload.AccessToken, nil
}

func (s *SessionService) setAuth(payload authPayload) {
	s.authMu.Lock()
	defer s.authMu.Unlock()
	s.accessToken = payload.AccessToken
	s.refreshToken = payload.RefreshToken
	s.accessExpiry = time.Now().Add(time.Duration(payload.ExpiresInSeconds) * time.Second)
	s.user = payload.User
}

func (s *SessionService) clearAuth() {
	s.authMu.Lock()
	defer s.authMu.Unlock()
	s.accessToken = ""
	s.refreshToken = ""
	s.accessExpiry = time.Time{}
	s.user = AuthUser{}
}

func decodeAuthPayload(resp *http.Response) (authPayload, error) {
	var envelope struct {
		Data authPayload `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return authPayload{}, err
	}
	if envelope.Data.AccessToken == "" || envelope.Data.RefreshToken == "" || envelope.Data.User.ID == "" {
		return authPayload{}, fmt.Errorf("incomplete authentication response")
	}
	return envelope.Data, nil
}

func responseError(resp *http.Response) error {
	var envelope struct {
		Message string `json:"message"`
		Reason  string `json:"reason"`
	}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	_ = json.Unmarshal(data, &envelope)
	if envelope.Message == "" {
		envelope.Message = http.StatusText(resp.StatusCode)
	}
	return fmt.Errorf("%s", envelope.Message)
}
