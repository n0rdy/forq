package services

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	sessionExpiryDurationMs = 7 * 24 * 60 * 60 * 1000 // 7 days in milliseconds
)

type SessionsService struct {
	activeSessions map[string]int64
	mu             sync.RWMutex
	ticker         *time.Ticker
}

func NewSessionsService() *SessionsService {
	ticker := time.NewTicker(1 * time.Hour)

	ss := &SessionsService{
		activeSessions: make(map[string]int64),
		mu:             sync.RWMutex{},
		ticker:         ticker,
	}

	go func() {
		for now := range ticker.C {
			ss.mu.Lock()
			for sessionId, expiry := range ss.activeSessions {
				if expiry < now.UnixMilli() {
					delete(ss.activeSessions, sessionId)
				}
			}
			ss.mu.Unlock()
		}
	}()

	return ss
}

func (ss *SessionsService) CreateSession() (string, int64) {
	sessionId := uuid.New().String()
	expiresAt := time.Now().UnixMilli() + sessionExpiryDurationMs

	ss.mu.Lock()
	ss.activeSessions[sessionId] = expiresAt
	ss.mu.Unlock()

	return sessionId, expiresAt
}

func (ss *SessionsService) IsSessionValid(sessionId string) bool {
	ss.mu.RLock()
	defer ss.mu.RUnlock()

	if expiry, exists := ss.activeSessions[sessionId]; exists {
		if time.Now().UnixMilli() < expiry {
			return true
		}
	}
	return false
}

func (ss *SessionsService) InvalidateSession(sessionId string) {
	ss.mu.Lock()
	delete(ss.activeSessions, sessionId)
	ss.mu.Unlock()
}

func (ss *SessionsService) Close() error {
	ss.ticker.Stop()
	return nil
}
