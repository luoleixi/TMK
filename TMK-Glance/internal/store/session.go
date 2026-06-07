package store

import (
	"sync"
	"time"

	"tmk-glance/internal/model"
)

type SessionStore struct {
	mu           sync.RWMutex
	sessions     map[string]*model.Session
	records      map[string][]model.Record
}

func NewSessionStore() *SessionStore {
	return &SessionStore{
		sessions: make(map[string]*model.Session),
		records:  make(map[string][]model.Record),
	}
}

func (s *SessionStore) Create(ses *model.Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[ses.ID] = ses
	s.records[ses.ID] = make([]model.Record, 0)
}

func (s *SessionStore) Get(id string) (*model.Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ses, ok := s.sessions[id]
	return ses, ok
}

func (s *SessionStore) Activate(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	ses, ok := s.sessions[id]
	if !ok || ses.Status != "ready" {
		return false
	}
	ses.Status = "active"
	return true
}

func (s *SessionStore) End(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	ses, ok := s.sessions[id]
	if !ok || ses.Status != "active" {
		return false
	}
	now := time.Now()
	ses.Status = "completed"
	ses.EndedAt = &now
	return true
}

func (s *SessionStore) Fail(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	ses, ok := s.sessions[id]
	if !ok || ses.Status != "active" {
		return false
	}
	now := time.Now()
	ses.Status = "error"
	ses.EndedAt = &now
	return true
}

func (s *SessionStore) AddRecord(sessionID string, r model.Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.ID = len(s.records[sessionID]) + 1
	s.records[sessionID] = append(s.records[sessionID], r)
	if ses, ok := s.sessions[sessionID]; ok {
		ses.RecordCount = len(s.records[sessionID])
	}
}

func (s *SessionStore) Records(sessionID string) ([]model.Record, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	recs, ok := s.records[sessionID]
	return recs, ok
}
