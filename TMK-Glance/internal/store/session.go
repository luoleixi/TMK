package store

import (
	"sync"
	"time"

	"tmk-glance/internal/model"
)

type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*model.Session
}

func NewSessionStore() *SessionStore {
	return &SessionStore{sessions: make(map[string]*model.Session)}
}

func (s *SessionStore) Create(ses *model.Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[ses.ID] = ses
}

func (s *SessionStore) Get(id string) (*model.Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ses, ok := s.sessions[id]
	return ses, ok
}

func (s *SessionStore) End(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	ses, ok := s.sessions[id]
	if !ok {
		return false
	}
	now := time.Now()
	ses.Status = "ended"
	ses.EndedAt = &now
	return true
}
