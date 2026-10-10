package handler

import (
	"testing"
	"time"
)

func TestRateLimiter_IsLocked_NotLocked(t *testing.T) {
	rl := NewRateLimiter()
	ip := "192.168.1.1"

	if rl.IsLocked(ip) {
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

	if !rl.IsLocked(ip) {
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

	if rl.IsLocked(ip) {
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

	if !rl.IsLocked(ip) {
		t.Error("Expected IP to be locked")
	}

	// Wait for lockout to expire
	time.Sleep(150 * time.Millisecond)

	if rl.IsLocked(ip) {
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
	if rl.IsLocked(ip1) && rl.IsLocked(ip2) {
		t.Error("Expected second IP to not be locked")
	}

	rl.RecordFailedAttempt(ip2)
	if rl.IsLocked(ip2) {
		t.Error("Expected second IP to not be locked after single attempt")
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
	if !rl.IsLocked(ip) {
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
