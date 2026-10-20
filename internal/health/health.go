// Package health provides HTTP health and readiness handlers used for
// orchestration and monitoring. Handlers report the overall service health
// and readiness of critical dependencies such as the database.
package health

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// HealthChecker runs health checks against service dependencies.
// It holds any clients/resources required to perform the checks (e.g. DB).
type HealthChecker struct {
	db *sql.DB
}

// HealthStatus represents the aggregated health of the application and each
// individual check. Timestamp should be interpreted by clients for staleness.
type HealthStatus struct {
	Status    string           `json:"status"`
	Timestamp time.Time        `json:"timestamp"`
	Checks    map[string]Check `json:"checks"`
}

// Check describes an individual dependency check result, including an
// optional human-readable message and the observed latency for the probe.
type Check struct {
	Status  string        `json:"status"`
	Message string        `json:"message,omitempty"`
	Latency time.Duration `json:"latency"`
}

// NewHealthChecker creates a HealthChecker using the provided database handle.
// The DB handle is used to verify connectivity for readiness and health checks.
func NewHealthChecker(db *sql.DB) *HealthChecker {
	return &HealthChecker{db: db}
}

// LivenessHandler reports that the process is running without checking dependencies.
func LivenessHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// HealthHandler is an HTTP handler that returns the system's overall health.
// It performs a set of checks (currently database) and returns a JSON
// payload with detailed results. If any check is unhealthy the handler
// responds with HTTP 503 Service Unavailable, otherwise 200 OK.
func (h *HealthChecker) HealthHandler(w http.ResponseWriter, r *http.Request) {
	// Use a reasonable timeout so health checks don't hang the request.
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// Run health checks that return the aggregated status and per-check data.
	status := h.checkHealth(ctx)

	// Set response content type and status code depending on the result.
	w.Header().Set("Content-Type", "application/json")
	if status.Status != "healthy" {
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	// Encode the full health payload. In production you might want to
	// handle encoding errors and log them; this implementation mirrors the
	// existing project's pattern of a simple encoder call.
	if err := json.NewEncoder(w).Encode(status); err != nil {
		slog.ErrorContext(r.Context(), "Operation failed", "operation", "encode health response", "error", err.Error())
	}
}

// ReadinessHandler checks whether the application is ready to receive
// traffic. Typically used by readiness probes in container orchestrators.
// For now it performs a lightweight DB ping with a short timeout and
// returns status/latency information.
func (h *HealthChecker) ReadinessHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	// Short timeout for readiness probe so orchestrators can make quick
	// decisions about pod readiness.
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	// Ping the database and measure latency. The latency is useful for
	// diagnostics in monitoring dashboards.
	start := time.Now()
	err := h.db.PingContext(ctx)
	latency := time.Since(start)

	if err != nil {
		// If DB is not reachable mark as not ready and include latency.
		w.WriteHeader(http.StatusServiceUnavailable)
		if err := json.NewEncoder(w).Encode(map[string]any{
			"status":  "not ready",
			"error":   "database connection failed",
			"latency": latency,
		}); err != nil {
			slog.ErrorContext(r.Context(), "Operation failed", "operation", "encode readiness response", "error", err.Error())
		}
		return
	}

	// Database reachable — report readiness and measured latency.
	if err := json.NewEncoder(w).Encode(map[string]any{
		"status":  "ready",
		"latency": latency,
	}); err != nil {
		slog.ErrorContext(r.Context(), "Operation failed", "operation", "encode readiness response", "error", err.Error())
	}
}

// checkHealth runs all configured checks and aggregates their results into
// a HealthStatus. Additional checks (cache, external APIs, etc.) can be
// added here and included in the returned Checks map.
func (h *HealthChecker) checkHealth(ctx context.Context) HealthStatus {
	checks := make(map[string]Check)
	overallStatus := "healthy"

	// Database check — recorded under the "database" key so callers can
	// inspect individual dependency health.
	dbCheck := h.checkDatabase(ctx)
	checks["database"] = dbCheck
	if dbCheck.Status != "healthy" {
		overallStatus = "unhealthy"
	}

	return HealthStatus{
		Status:    overallStatus,
		Timestamp: time.Now(),
		Checks:    checks,
	}
}

// checkDatabase performs a connectivity probe to the configured database
// and returns a Check with the observed latency and an optional message on
// failure. The function is intentionally lightweight so it can be used in
// both health and readiness probes.
func (h *HealthChecker) checkDatabase(ctx context.Context) Check {
	start := time.Now()
	err := h.db.PingContext(ctx)
	latency := time.Since(start)

	if err != nil {
		return Check{
			Status:  "unhealthy",
			Message: "database connection failed",
			Latency: latency,
		}
	}

	return Check{
		Status:  "healthy",
		Latency: latency,
	}
}
