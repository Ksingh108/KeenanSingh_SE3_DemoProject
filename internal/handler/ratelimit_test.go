package handler

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestBookingRequestLimiter_ConcurrentRequests(t *testing.T) {
	var limiter bookingRequestLimiter
	now := time.Now()
	accepted := make(chan struct{}, 64)
	var requests sync.WaitGroup
	for attempt := 0; attempt < cap(accepted); attempt++ {
		requests.Add(1)
		go func() {
			defer requests.Done()
			if limiter.allow("198.51.100.7", now) == 0 {
				accepted <- struct{}{}
			}
		}()
	}
	requests.Wait()
	if len(accepted) != bookingRequestsPerMinute {
		t.Fatalf("accepted %d concurrent requests, want %d", len(accepted), bookingRequestsPerMinute)
	}
	if retryAfter := limiter.allow("198.51.100.7", now.Add(500*time.Millisecond)); retryAfter != 60 {
		t.Fatalf("Retry-After = %d, want 60", retryAfter)
	}
	if retryAfter := limiter.allow("198.51.100.7", now.Add(time.Minute)); retryAfter != 0 {
		t.Fatalf("request after expiry rejected with Retry-After %d", retryAfter)
	}
}

func TestBookingRequestLimiter_BoundedStateAndCleanup(t *testing.T) {
	now := time.Now()
	limiter := bookingRequestLimiter{clients: make(map[string]bookingRequestWindow)}
	for client := 0; client < maxBookingRateClients; client++ {
		limiter.clients[strconv.Itoa(client)] = bookingRequestWindow{count: 1, resetAt: now.Add(time.Minute)}
	}
	if limiter.allow("new client", now) == 0 {
		t.Fatal("accepted new client despite full tracking map")
	}
	if len(limiter.clients) != maxBookingRateClients {
		t.Fatalf("tracking map grew to %d clients", len(limiter.clients))
	}
	if limiter.allow("0", now) != 0 {
		t.Fatal("existing client with quota was rejected")
	}
	if limiter.allow("new client", now.Add(time.Minute)) != 0 {
		t.Fatal("new client rejected after expired records were cleaned up")
	}
	if len(limiter.clients) != 1 {
		t.Fatalf("cleanup left %d clients, want 1", len(limiter.clients))
	}
}

func TestRateLimiter_IsLocked_NotLocked(t *testing.T) {
	rl := NewRateLimiter()
	ip := "192.168.1.1"

	if rl.IsBlocked(ip) {
		t.Error("Expected IP to not be locked initially")
	}
}

func TestRateLimiter_RecordFailedAttempt_LockoutAfterThreshold(t *testing.T) {
	rl := NewRateLimiter()
	ip := "192.168.1.1"

	// Record failures up to threshold
	for i := 0; i < rl.maxFailedAttempts; i++ {
		rl.RecordFailedAttempt(ip)
	}

	if !rl.IsBlocked(ip) {
		t.Error("Expected IP to be locked after max failed attempts")
	}
}

func TestRateLimiter_RecordSuccessfulAttempt_ResetsCounter(t *testing.T) {
	rl := NewRateLimiter()
	ip := "192.168.1.1"

	// Record some failures
	for i := 0; i < 3; i++ {
		rl.RecordFailedAttempt(ip)
	}

	// Record successful attempt to reset
	rl.RecordSuccessfulAttempt(ip)

	if rl.IsBlocked(ip) {
		t.Error("Expected IP to not be locked after successful attempt")
	}
}

func TestRateLimiter_LockoutExpires(t *testing.T) {
	rl := NewRateLimiter()
	rl.lockoutDuration = 100 * time.Millisecond // Short duration for testing
	ip := "192.168.1.1"

	// Lock out the IP
	for i := 0; i < rl.maxFailedAttempts; i++ {
		rl.RecordFailedAttempt(ip)
	}

	if !rl.IsBlocked(ip) {
		t.Error("Expected IP to be locked")
	}

	// Wait for lockout to expire
	time.Sleep(150 * time.Millisecond)

	if rl.IsBlocked(ip) {
		t.Error("Expected IP to be unlocked after lockout duration expires")
	}
}

func TestRateLimiter_MultipleIPs(t *testing.T) {
	rl := NewRateLimiter()
	ip1 := "192.168.1.1"
	ip2 := "192.168.1.2"

	// Lock out first IP
	for i := 0; i < rl.maxFailedAttempts; i++ {
		rl.RecordFailedAttempt(ip1)
	}

	// Second IP should not be affected
	if rl.IsBlocked(ip1) && rl.IsBlocked(ip2) {
		t.Error("Expected second IP to not be locked")
	}

	rl.RecordFailedAttempt(ip2)
	if rl.IsBlocked(ip2) {
		t.Error("Expected second IP to not be locked after single attempt")
	}
}

func TestRateLimiter_BoundedStateRejectsNewIPs(t *testing.T) {
	rl := NewRateLimiter()
	now := time.Now()
	for ip := 0; ip < maxTrackedLoginIPs; ip++ {
		rl.attempts[strconv.Itoa(ip)] = &attemptRecord{failedCount: 1, lastAttempt: now}
	}
	rl.nextCleanup = now.Add(time.Hour)

	if !rl.IsBlocked("new-ip") {
		t.Fatal("new IP was not rejected while tracking capacity was full")
	}
	if rl.RecordFailedAttempt("new-ip") {
		t.Fatal("recorded a new IP despite full tracking capacity")
	}
	if len(rl.attempts) != maxTrackedLoginIPs {
		t.Fatalf("tracking map grew to %d entries", len(rl.attempts))
	}
	if rl.IsBlocked("0") {
		t.Fatal("existing tracked IP was rejected despite not being locked")
	}
}

func TestRateLimiter_CleansStaleRecordsBeforeRejectingNewIPs(t *testing.T) {
	rl := NewRateLimiter()
	now := time.Now()
	lastAttempt := now.Add(-2*rl.attemptResetDuration - time.Second)
	for ip := 0; ip < maxTrackedLoginIPs; ip++ {
		rl.attempts[strconv.Itoa(ip)] = &attemptRecord{failedCount: 1, lastAttempt: lastAttempt}
	}
	rl.nextCleanup = now.Add(-time.Second)

	if rl.IsBlocked("new-ip") {
		t.Fatal("new IP was rejected after stale records were eligible for cleanup")
	}
	if len(rl.attempts) != 0 {
		t.Fatalf("cleanup left %d stale entries", len(rl.attempts))
	}
}

func TestRateLimiter_PeriodicCleanup_RemovesExpiredRecords(t *testing.T) {
	rl := NewRateLimiter()
	rl.lockoutDuration = 100 * time.Millisecond
	rl.attemptResetDuration = 50 * time.Millisecond
	ip := "192.168.1.1"
	defer rl.Stop()

	// Record failures to lock out IP
	for i := 0; i < rl.maxFailedAttempts; i++ {
		rl.RecordFailedAttempt(ip)
	}

	// Verify locked
	if !rl.IsBlocked(ip) {
		t.Error("Expected IP to be locked")
	}

	// Wait for lockout and attempts to expire
	time.Sleep(300 * time.Millisecond)

	// Manually call cleanup to remove expired records
	rl.removeExpiredRecords()

	// Verify record was removed from map
	rl.mu.RLock()
	_, exists := rl.attempts[ip]
	rl.mu.RUnlock()

	if exists {
		t.Error("Expected expired record to be removed from attempts map")
	}
}

func TestRateLimiter_StopIsIdempotentAndWaitsForCleanup(t *testing.T) {
	rl := NewRateLimiter()
	rl.StartPeriodicCleanup()
	rl.Stop()
	rl.Stop()

	select {
	case <-rl.cleanupDone:
	default:
		t.Fatal("cleanup goroutine is still running after Stop")
	}
}

func TestClientIPFromRequest_IgnoresForwardingHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/admin/login", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.8, 10.0.0.5")
	req.Header.Set("X-Real-IP", "203.0.113.9")

	for _, remote := range []string{"198.51.100.7:1234", "127.0.0.1:1234"} {
		req.RemoteAddr = remote
		want := clientIPFromRemoteAddr(remote)
		if got := NewRateLimiter().clientIPFromRequest(req); got != want {
			t.Fatalf("clientIPFromRequest() = %q, want %q", got, want)
		}
	}
}
