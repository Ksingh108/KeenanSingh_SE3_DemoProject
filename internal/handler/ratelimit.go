package handler

import (
	"sync"
	"time"
)

// RateLimiter tracks failed login attempts per IP to prevent brute-force attacks.
// After MaxFailedAttempts consecutive failures, the IP is locked out for LockoutDuration.
type RateLimiter struct {
	mu                   sync.RWMutex
	attempts             map[string]*attemptRecord // IP -> attempt record
	maxFailedAttempts    int
	lockoutDuration      time.Duration
	attemptResetDuration time.Duration
	cleanupTicker        *time.Ticker
	stopCleanup          chan struct{}
}

type attemptRecord struct {
	failedCount int
	lastAttempt time.Time
	lockedUntil time.Time
}

// NewRateLimiter creates a rate limiter with default settings: 5 failed attempts,
// 15-minute lockout, 10-minute attempt counter reset.
func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		attempts:             make(map[string]*attemptRecord),
		maxFailedAttempts:    5,
		lockoutDuration:      15 * time.Minute,
		attemptResetDuration: 10 * time.Minute,
		stopCleanup:          make(chan struct{}),
	}
}

// IsLocked returns true if the IP is currently locked out due to too many failed attempts.
func (rl *RateLimiter) IsLocked(ip string) bool {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	record, exists := rl.attempts[ip]
	if !exists {
		return false
	}

	// Check if lockout period has expired
	if time.Now().After(record.lockedUntil) {
		return false
	}

	return true
}

// RecordFailedAttempt increments the failed attempt counter for an IP.
// After MaxFailedAttempts, the IP is locked out for LockoutDuration.
func (rl *RateLimiter) RecordFailedAttempt(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	record, exists := rl.attempts[ip]
	if !exists {
		record = &attemptRecord{}
		rl.attempts[ip] = record
	}

	// Reset counter if attemptResetDuration has passed
	if time.Since(record.lastAttempt) > rl.attemptResetDuration {
		record.failedCount = 0
	}

	record.failedCount++
	record.lastAttempt = time.Now()

	// Lock out IP if threshold exceeded
	if record.failedCount >= rl.maxFailedAttempts {
		record.lockedUntil = time.Now().Add(rl.lockoutDuration)
	}
}

// RecordSuccessfulAttempt resets the failed attempt counter for an IP after successful login.
func (rl *RateLimiter) RecordSuccessfulAttempt(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	delete(rl.attempts, ip)
}

// StartPeriodicCleanup begins a goroutine that removes expired records every hour.
// Call Stop() to gracefully terminate the cleanup routine.
func (rl *RateLimiter) StartPeriodicCleanup() {
	rl.cleanupTicker = time.NewTicker(1 * time.Hour)
	go func() {
		for {
			select {
			case <-rl.cleanupTicker.C:
				rl.removeExpiredRecords()
			case <-rl.stopCleanup:
				rl.cleanupTicker.Stop()
				return
			}
		}
	}()
}

// Stop gracefully stops the periodic cleanup goroutine.
func (rl *RateLimiter) Stop() {
	if rl.cleanupTicker != nil {
		close(rl.stopCleanup)
	}
}

// removeExpiredRecords deletes all records with expired lockouts or stale attempt counters.
func (rl *RateLimiter) removeExpiredRecords() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	for ip, record := range rl.attempts {
		// Remove if lockout has expired AND no recent attempts
		lockoutExpired := now.After(record.lockedUntil)
		attemptStale := now.Sub(record.lastAttempt) > rl.attemptResetDuration*2
		if lockoutExpired && attemptStale {
			delete(rl.attempts, ip)
		}
	}
}
