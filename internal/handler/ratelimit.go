package handler

import (
	"net/http"
	"sync"
	"time"
)

const (
	bookingRequestsPerMinute = 5
	maxBookingRateClients    = 10000
	maxTrackedLoginIPs       = 10000
)

type bookingRequestWindow struct {
	count   int
	resetAt time.Time
}

type bookingRequestLimiter struct {
	mu          sync.Mutex
	clients     map[string]bookingRequestWindow
	nextCleanup time.Time
}

func (limiter *bookingRequestLimiter) allow(ip string, now time.Time) int {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	if limiter.clients == nil {
		limiter.clients = make(map[string]bookingRequestWindow)
	}
	if !now.Before(limiter.nextCleanup) {
		for client, window := range limiter.clients {
			if !now.Before(window.resetAt) {
				delete(limiter.clients, client)
			}
		}
		limiter.nextCleanup = now.Add(time.Minute)
	}
	window, exists := limiter.clients[ip]
	if !exists && len(limiter.clients) >= maxBookingRateClients {
		return 60
	}
	if !exists || !now.Before(window.resetAt) {
		window = bookingRequestWindow{resetAt: now.Add(time.Minute)}
	}
	if window.count >= bookingRequestsPerMinute {
		return int((window.resetAt.Sub(now) + time.Second - 1) / time.Second)
	}
	window.count++
	limiter.clients[ip] = window
	return 0
}

// RateLimiter tracks failed login attempts per IP to prevent brute-force attacks.
// It bounds tracked IPs and rejects new ones when the limit is reached. Clients
// are identified by connection IP only; forwarding headers are ignored (see ADR-0006).
type RateLimiter struct {
	mu                   sync.RWMutex
	cleanupMu            sync.Mutex
	attempts             map[string]*attemptRecord // IP -> attempt record
	maxFailedAttempts    int
	lockoutDuration      time.Duration
	attemptResetDuration time.Duration
	nextCleanup          time.Time
	cleanupTicker        *time.Ticker
	stopCleanup          chan struct{}
	cleanupDone          chan struct{}
	cleanupStarted       bool
	cleanupStopped       bool
}

type attemptRecord struct {
	failedCount int
	lastAttempt time.Time
	lockedUntil time.Time
}

// NewRateLimiter creates a rate limiter with default settings: 5 failed attempts,
// a 15-minute lockout, a 10-minute attempt counter reset, and a 10,000-IP tracking limit.
func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		attempts:             make(map[string]*attemptRecord),
		maxFailedAttempts:    5,
		lockoutDuration:      15 * time.Minute,
		attemptResetDuration: 10 * time.Minute,
		stopCleanup:          make(chan struct{}),
		cleanupDone:          make(chan struct{}),
	}
}

// IsBlocked reports whether the IP is locked out or cannot be tracked at capacity.
func (rl *RateLimiter) IsBlocked(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	if !now.Before(rl.nextCleanup) {
		rl.removeExpiredRecordsLocked(now)
		rl.nextCleanup = now.Add(time.Minute)
	}

	record, exists := rl.attempts[ip]
	if !exists && len(rl.attempts) >= maxTrackedLoginIPs {
		return true
	}
	if !exists {
		return false
	}

	// Check if lockout period has expired
	if now.After(record.lockedUntil) {
		return false
	}

	return true
}

// RecordFailedAttempt increments the failed attempt counter for an IP. It returns
// false if a new IP cannot be tracked because the limiter is at capacity.
func (rl *RateLimiter) RecordFailedAttempt(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	record, exists := rl.attempts[ip]
	if !exists {
		if len(rl.attempts) >= maxTrackedLoginIPs {
			return false
		}
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
	return true
}

// RecordSuccessfulAttempt resets the failed attempt counter for an IP after successful login.
func (rl *RateLimiter) RecordSuccessfulAttempt(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	delete(rl.attempts, ip)
}

// StartPeriodicCleanup begins a goroutine that removes expired records every minute.
// Call Stop() to gracefully terminate the cleanup routine.
func (rl *RateLimiter) StartPeriodicCleanup() {
	rl.cleanupMu.Lock()
	if rl.cleanupStarted || rl.cleanupStopped {
		rl.cleanupMu.Unlock()
		return
	}
	rl.cleanupStarted = true
	rl.cleanupTicker = time.NewTicker(time.Minute)
	ticker := rl.cleanupTicker
	rl.cleanupMu.Unlock()

	go func() {
		defer close(rl.cleanupDone)
		for {
			select {
			case <-ticker.C:
				rl.removeExpiredRecords()
			case <-rl.stopCleanup:
				ticker.Stop()
				return
			}
		}
	}()
}

// Stop gracefully stops the periodic cleanup goroutine.
func (rl *RateLimiter) Stop() {
	rl.cleanupMu.Lock()
	if !rl.cleanupStopped {
		rl.cleanupStopped = true
		close(rl.stopCleanup)
	}
	started := rl.cleanupStarted
	rl.cleanupMu.Unlock()
	if started {
		<-rl.cleanupDone
	}
}

// removeExpiredRecords deletes all records with expired lockouts or stale attempt counters.
func (rl *RateLimiter) removeExpiredRecords() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	rl.removeExpiredRecordsLocked(now)
	rl.nextCleanup = now.Add(time.Minute)
}

func (rl *RateLimiter) removeExpiredRecordsLocked(now time.Time) {
	for ip, record := range rl.attempts {
		// Remove if lockout has expired AND no recent attempts
		lockoutExpired := now.After(record.lockedUntil)
		attemptStale := now.Sub(record.lastAttempt) > rl.attemptResetDuration*2
		if lockoutExpired && attemptStale {
			delete(rl.attempts, ip)
		}
	}
}

// clientIPFromRequest returns the connection IP. Forwarding headers are ignored so a
// client cannot choose its own lockout identity.
func (rl *RateLimiter) clientIPFromRequest(r *http.Request) string {
	return clientIPFromRemoteAddr(r.RemoteAddr)
}
