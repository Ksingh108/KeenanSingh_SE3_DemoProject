package memory

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/GringottsBankBranchBooking/internal/domain"
)

// SessionRepository keeps sessions in memory, keyed by token hash. It is
// safe for concurrent use: the auth middleware and the periodic cleanup
// goroutine access it from different goroutines.
type SessionRepository struct {
	mu       sync.RWMutex
	sessions map[string]*domain.AdminSession
}

func NewSessionRepository() *SessionRepository {
	return &SessionRepository{
		sessions: make(map[string]*domain.AdminSession),
	}
}

func (r *SessionRepository) Create(ctx context.Context, session *domain.AdminSession) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.sessions[session.TokenHash] = session
	return nil
}

func (r *SessionRepository) GetByTokenHash(ctx context.Context, tokenHash string) (*domain.AdminSession, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	session, exists := r.sessions[tokenHash]
	if !exists {
		return nil, errors.New("session not found")
	}
	return session, nil
}

func (r *SessionRepository) Delete(ctx context.Context, tokenHash string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.sessions, tokenHash)
	return nil
}

func (r *SessionRepository) CleanupExpired(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	for tokenHash, session := range r.sessions {
		if session.ExpiresAt.Before(now) {
			delete(r.sessions, tokenHash)
		}
	}
	return nil
}
