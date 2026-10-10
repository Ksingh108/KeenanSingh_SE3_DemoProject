package memory

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/GringottsBankBranchBooking/internal/domain"
)

// AuditRepository keeps audit entries in memory. It is safe for concurrent use.
type AuditRepository struct {
	mu      sync.RWMutex
	entries []*domain.AuditEntry
}

func NewAuditRepository() *AuditRepository {
	return &AuditRepository{}
}

func (r *AuditRepository) Create(_ context.Context, entry *domain.AuditEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry.ID = strconv.Itoa(len(r.entries) + 1)
	entry.CreatedAt = time.Now()

	// Store a copy so later changes by the caller cannot rewrite history.
	stored := *entry
	r.entries = append(r.entries, &stored)
	return nil
}

// Entries returns a copy of all recorded entries in the order they were
// created. The AuditRepository interface has no read path (the audit trail is
// append-only for the application), so this exists for tests and inspection.
func (r *AuditRepository) Entries() []domain.AuditEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entries := make([]domain.AuditEntry, len(r.entries))
	for i, entry := range r.entries {
		entries[i] = *entry
	}
	return entries
}
