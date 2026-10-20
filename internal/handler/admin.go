// Package handler provides HTTP handlers for the Gringotts Bank Branch Booking System.
// This package implements the presentation layer of Clean Architecture, handling HTTP requests,
// input validation, and response formatting while delegating business logic to service layer.
package handler

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"

	"github.com/gringottsbankbranchbooking/internal/apierror"
	"github.com/gringottsbankbranchbooking/internal/service"
)

const maxLoginBodyBytes = 16 * 1024

// AdminHandler handles HTTP requests for administrative operations including authentication,
// session management, and admin-only branch management operations.
// It delegates business logic to the service layer
// and focusing solely on HTTP concerns (request parsing, validation, response formatting).
//
// Security Considerations:
// - All admin operations require valid authentication tokens
// - Secure logging prevents log injection vulnerabilities
// - HTTP-only cookies protect against XSS attacks
// - Login lockout slows brute-force attacks (in-memory, per server process)
type AdminHandler struct {
	authService *service.AuthService // Handles admin authentication and session management
	rateLimiter *RateLimiter         // Login lockout; in-memory, per process
}

// NewAdminHandler creates a new AdminHandler with required service dependencies.
// Uses dependency injection pattern for testability and loose coupling.
// Starts periodic cleanup of expired rate limit records to prevent memory leaks.
//
// Parameters:
//   - authService: Service for admin authentication and session management
//
// Returns:
//   - *AdminHandler: Configured handler ready for HTTP routing
func NewAdminHandler(authService *service.AuthService) *AdminHandler {
	rateLimiter := NewRateLimiter()
	rateLimiter.StartPeriodicCleanup()
	return &AdminHandler{
		authService: authService,
		rateLimiter: rateLimiter,
	}
}

// Stop terminates the handler's background rate-limit cleanup routine.
func (h *AdminHandler) Stop() {
	h.rateLimiter.Stop()
}

// Login handles admin authentication requests via HTTP POST.
// Validates credentials, creates secure session tokens, and sets HTTP-only cookies.
// Used by the admin login form on the admin page.
//
// Request Body (JSON):
//   - username: Admin username used for authentication
//   - password: Admin password, always verified with bcrypt (a dummy hash is used for unknown usernames)
//
// Response:
//   - 200 OK: {"status": "success"} with secure HTTP-only cookie
//   - 400 Bad Request: Invalid JSON request format
//   - 401 Unauthorized: Invalid credentials (generic message for security)
//   - 413 Request Entity Too Large: Body exceeds 16 KiB
//   - 415 Unsupported Media Type: Content-Type is not application/json
//   - 429 Too Many Requests: Client IP is locked out after repeated failures
//   - 500 Internal Server Error: Authentication backend failure (not counted as a failed attempt)
//   - 503 Service Unavailable: Database unavailable (Retry-After: 1)
//
// Security Features:
//   - Secure logging prevents log injection vulnerabilities
//   - HTTP-only cookies protect against XSS attacks
//   - Generic 401 response body; unknown usernames do the same bcrypt work as known ones
//   - Session tokens expire after 24 hours
func (h *AdminHandler) Login(w http.ResponseWriter, r *http.Request) {
	clientIP := h.rateLimiter.clientIPFromRequest(r)

	// Check if client is currently locked out due to too many failed attempts
	if h.rateLimiter.IsBlocked(clientIP) {
		slog.InfoContext(r.Context(), "Login attempt blocked by rate limiter", "client_ip", clientIP)
		apierror.Write(w, http.StatusTooManyRequests, "Login temporarily unavailable. Please try again later.")
		return
	}
	if !requireJSONContentType(w, r) {
		return
	}

	// Decode the submitted credentials from JSON.
	var req struct {
		Username string `json:"username"` // Admin username for authentication
		Password string `json:"password"` // Admin password for authentication
	}

	// Parse the JSON request body.
	r.Body = http.MaxBytesReader(w, r.Body, maxLoginBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var sizeError *http.MaxBytesError
		if errors.As(err, &sizeError) {
			apierror.Write(w, http.StatusRequestEntityTooLarge, "Login request body exceeds 16 KiB")
			return
		}
		apierror.Write(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if err := json.Unmarshal(body, &req); err != nil {
		apierror.Write(w, http.StatusBadRequest, "Invalid request")
		return
	}

	// Delegate authentication to service layer for business logic
	token, err := h.authService.Login(r.Context(), req.Username, req.Password)
	if err != nil {
		if !errors.Is(err, service.ErrInvalidCredentials) {
			// Internal failure, not a bad guess: don't count it towards the lockout.
			slog.ErrorContext(r.Context(), "Operation failed", "operation", "admin login", "error", err.Error())
			apierror.WriteFailure(w, err, "Internal server error")
			return
		}
		// Log the client IP only, never the submitted username or password.
		slog.WarnContext(r.Context(), "Login failed", "client_ip", clientIP)
		if !h.rateLimiter.RecordFailedAttempt(clientIP) {
			apierror.Write(w, http.StatusTooManyRequests, "Login temporarily unavailable. Please try again later.")
			return
		}
		apierror.Write(w, http.StatusUnauthorized, "Invalid credentials")
		return
	}

	// Record successful login to reset rate limit counter
	h.rateLimiter.RecordSuccessfulAttempt(clientIP)

	// Set secure HTTP-only cookie with session token
	http.SetCookie(w, &http.Cookie{
		Name:     "admin_token",
		Value:    token,
		HttpOnly: true,                    // Prevents XSS attacks
		Secure:   true,                    // Only transmit over HTTPS
		Path:     "/",                     // Cookie available for all admin routes
		MaxAge:   86400,                   // 24 hours session duration
		SameSite: http.SameSiteStrictMode, // Prevents cross-site cookie transmission
	})

	// Return success response in JSON format
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "success"}); err != nil {
		slog.ErrorContext(r.Context(), "Operation failed", "operation", "encode login response", "error", err.Error())
	}
}

func clientIPFromRemoteAddr(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

// Logout handles admin session termination via HTTP POST.
// Invalidates session tokens and clears authentication cookies.
//
// Request: No body required, uses existing admin_token cookie
//
// Response:
//   - 200 OK: {"status": "logged out"} with cleared cookie
//
// Security Features:
//   - Server-side session invalidation prevents token reuse
//   - Cookie clearing ensures client-side cleanup
//   - Graceful handling of missing cookies (no error thrown)
func (h *AdminHandler) Logout(w http.ResponseWriter, r *http.Request) {
	var logoutErr error
	// Attempt to retrieve existing session token from cookie
	cookie, err := r.Cookie("admin_token")
	if err == nil {
		// Invalidate session token on server side for security
		logoutErr = h.authService.Logout(r.Context(), cookie.Value)
		if logoutErr != nil {
			slog.ErrorContext(r.Context(), "Operation failed", "operation", "logout session", "error", logoutErr.Error())
		}
	}

	// Clear authentication cookie by setting expired date
	http.SetCookie(w, &http.Cookie{
		Name:     "admin_token",
		Value:    "",                      // Empty value
		HttpOnly: true,                    // Maintain security settings
		Secure:   true,                    // Match original cookie security settings
		Path:     "/",                     // Same path as original cookie
		MaxAge:   -1,                      // Negative MaxAge expires cookie immediately
		SameSite: http.SameSiteStrictMode, // Match original cookie settings
	})
	if logoutErr != nil {
		apierror.WriteFailure(w, logoutErr, "Internal server error")
		return
	}

	// Return logout confirmation in JSON format
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "logged out"}); err != nil {
		slog.ErrorContext(r.Context(), "Operation failed", "operation", "encode logout response", "error", err.Error())
	}
}

// AdminPage redirects authenticated administrators to the branch management page.
// This endpoint is protected by RequireAuth middleware.
//
// Request: GET /admin/ (requires valid admin_token cookie)
//
// Response:
//   - 302 Found: Redirect to /admin/branches if authenticated
//   - 401 Unauthorized: JSON error if not authenticated (handled by middleware)
//
// Security:
//   - Protected by RequireAuth middleware
//   - Requires valid session token
//   - Returns a 401 JSON error if unauthorized
func (h *AdminHandler) AdminPage(w http.ResponseWriter, r *http.Request) {
	// Redirect to branches page as default admin page
	http.Redirect(w, r, "/admin/branches", http.StatusFound)
}

// AdminBranchesPage serves the branch management page for authenticated administrators.
// This endpoint provides the interface for managing bank branches including adding and deleting branches.
//
// Request: GET /admin/branches (requires valid admin_token cookie)
//
// Response:
//   - 200 OK: Branch management HTML page
//   - 401 Unauthorized: JSON error if not authenticated (handled by middleware)
func (h *AdminHandler) AdminBranchesPage(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "./web/static/admin-branches.html")
}

// AdminBookingsPage serves the booking management page for authenticated administrators.
// This endpoint provides the interface for viewing and managing customer bookings across all branches.
//
// Request: GET /admin/bookings (requires valid admin_token cookie)
//
// Response:
//   - 200 OK: Booking management HTML page
//   - 401 Unauthorized: JSON error if not authenticated (handled by middleware)
func (h *AdminHandler) AdminBookingsPage(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "./web/static/admin-bookings.html")
}
