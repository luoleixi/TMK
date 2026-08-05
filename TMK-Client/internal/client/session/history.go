package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

func (s *SessionService) ListHistory(offset, limit int) ([]HistorySession, int, error) {
	return s.SearchHistory(offset, limit, "", "", "")
}

func (s *SessionService) SearchHistory(offset, limit int, keyword, dateFrom, dateTo string) ([]HistorySession, int, error) {
	u, err := url.Parse(s.apiURL() + "/history")
	if err != nil {
		return nil, 0, fmt.Errorf("history url: %w", err)
	}
	q := u.Query()
	q.Set("offset", fmt.Sprint(offset))
	q.Set("limit", fmt.Sprint(limit))
	if keyword != "" {
		q.Set("keyword", keyword)
	}
	if dateFrom != "" {
		q.Set("date_from", dateFrom)
	}
	if dateTo != "" {
		q.Set("date_to", dateTo)
	}
	u.RawQuery = q.Encode()
	resp, err := s.httpClient.Get(u.String())
	if err != nil {
		return nil, 0, fmt.Errorf("list history: %w", err)
	}
	defer resp.Body.Close()
	var result struct {
		Data struct {
			Total    int              `json:"total"`
			Sessions []HistorySession `json:"sessions"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, 0, err
	}
	return result.Data.Sessions, result.Data.Total, nil
}

func (s *SessionService) GetHistory(sessionID string) (*HistoryDetail, error) {
	resp, err := s.httpClient.Get(s.apiURL() + "/history/" + url.PathEscape(sessionID))
	if err != nil {
		return nil, fmt.Errorf("get history: %w", err)
	}
	defer resp.Body.Close()
	var result struct {
		Data HistoryDetail `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if result.Data.SessionID == "" {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	return &result.Data, nil
}

func (s *SessionService) SummarizeHistory(sessionID string) (string, error) {
	resp, err := s.httpClient.Post(s.apiURL()+"/history/"+url.PathEscape(sessionID)+"/summary", "application/json", nil)
	if err != nil {
		return "", fmt.Errorf("summarize history: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("summarize history failed: status %d", resp.StatusCode)
	}
	var result struct {
		Data struct {
			Summary string `json:"summary"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.Data.Summary == "" {
		return "", fmt.Errorf("empty summary")
	}
	return result.Data.Summary, nil
}

func (s *SessionService) DeleteHistory(sessionID string) error {
	req, err := http.NewRequest(http.MethodDelete, s.apiURL()+"/history/"+url.PathEscape(sessionID), nil)
	if err != nil {
		return err
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("delete history: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("delete history failed: status %d", resp.StatusCode)
	}
	return nil
}

func (s *SessionService) DeleteHistoryBatch(ids []string) (int, error) {
	body, err := json.Marshal(map[string][]string{"ids": ids})
	if err != nil {
		return 0, err
	}
	resp, err := s.httpClient.Post(s.apiURL()+"/history/delete", "application/json", bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("delete history batch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return 0, fmt.Errorf("delete history batch failed: status %d", resp.StatusCode)
	}
	var result struct {
		Data struct {
			Deleted int `json:"deleted"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}
	return result.Data.Deleted, nil
}
