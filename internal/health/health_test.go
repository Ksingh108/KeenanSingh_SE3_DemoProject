package health

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "github.com/lib/pq"
)

func TestHealthChecker_HealthHandler(t *testing.T) {
	// Create a mock database connection (will fail ping, which is expected for testing)
	db, _ := sql.Open("postgres", "invalid://connection")
	defer db.Close()

	checker := NewHealthChecker(db)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	checker.HealthHandler(w, req)

	// Should return 503 since database connection will fail
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}
}

func TestHealthChecker_ReadinessHandler(t *testing.T) {
	// Create a mock database connection (will fail ping)
	db, _ := sql.Open("postgres", "invalid://connection")
	defer db.Close()

	checker := NewHealthChecker(db)

	req := httptest.NewRequest("GET", "/ready", nil)
	w := httptest.NewRecorder()

	checker.ReadinessHandler(w, req)

	// Should return 503 since database connection will fail
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}
}

func TestHealthChecker_checkDatabase(t *testing.T) {
	// Create a mock database connection
	db, _ := sql.Open("postgres", "invalid://connection")
	defer db.Close()

	checker := NewHealthChecker(db)
	ctx := context.Background()

	check := checker.checkDatabase(ctx)

	if check.Status != "unhealthy" {
		t.Errorf("Expected status unhealthy, got %s", check.Status)
	}

	if check.Message != "database connection failed" {
		t.Errorf("Expected message 'database connection failed', got %s", check.Message)
	}
}
