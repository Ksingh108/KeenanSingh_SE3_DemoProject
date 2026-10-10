// Package handler provides HTTP handlers for the Gringotts Bank Branch Booking System.
// This package implements the presentation layer of Clean Architecture, handling HTTP requests,
// input validation, and response formatting while delegating business logic to service layer.
package handler

import (
	"encoding/json"
	"net/http"

	"github.com/GringottsBankBranchBooking/internal/service"
	"log/slog"
)

// AdminHandler handles HTTP requests for administrative operations including authentication,
// session management, and admin-only branch management operations.
// It follows Clean Architecture principles by delegating business logic to service layer
// and focusing solely on HTTP concerns (request parsing, validation, response formatting).
//
// Security Considerations:
// - All admin operations require valid authentication tokens
// - Input validation prevents injection attacks
// - Secure logging prevents log injection vulnerabilities
// - HTTP-only cookies protect against XSS attacks
// - Rate limiting prevents brute-force attacks on login endpoint
type AdminHandler struct {
	authService   *service.AuthService   // Handles admin authentication and session management
	branchService *service.BranchService // Handles branch operations for admin management
	rateLimiter   *RateLimiter           // Prevents brute-force attacks on login
}

// NewAdminHandler creates a new AdminHandler with required service dependencies.
// Uses dependency injection pattern for testability and loose coupling.
// Starts periodic cleanup of expired rate limit records to prevent memory leaks.
//
// Parameters:
//   - authService: Service for admin authentication and session management
//   - branchService: Service for branch operations and management
//
// Returns:
//   - *AdminHandler: Configured handler ready for HTTP routing
func NewAdminHandler(authService *service.AuthService, branchService *service.BranchService) *AdminHandler {
	rateLimiter := NewRateLimiter()
	rateLimiter.StartPeriodicCleanup()
	return &AdminHandler{
		authService:   authService,
		branchService: branchService,
		rateLimiter:   rateLimiter,
	}
}

// Login handles admin authentication requests via HTTP POST.
// Validates credentials, creates secure session tokens, and sets HTTP-only cookies.
// Used by the admin login modal on the main page for secure authentication.
//
// Request Body (JSON):
//   - username: Admin username (required, validated for injection prevention)
//   - password: Admin password (required, securely hashed comparison)
//
// Response:
//   - 200 OK: {"status": "success"} with secure HTTP-only cookie
//   - 400 Bad Request: Invalid JSON request format
//   - 401 Unauthorized: Invalid credentials (generic message for security)
//
// Security Features:
//   - Input validation prevents injection attacks
//   - Secure logging prevents log injection vulnerabilities
//   - HTTP-only cookies protect against XSS attacks
//   - Generic error messages prevent username enumeration
//   - Session tokens expire after 24 hours
func (h *AdminHandler) Login(w http.ResponseWriter, r *http.Request) {
	// Extract client IP for rate limiting
	clientIP := r.RemoteAddr

	// Check if client is currently locked out due to too many failed attempts
	if h.rateLimiter.IsLocked(clientIP) {
		slog.Info("Login attempt from locked-out IP", "client_ip", clientIP)
		http.Error(w, "Too many failed attempts. Please try again later.", http.StatusTooManyRequests)
		return
	}

	// Define request structure for JSON parsing and validation
	var req struct {
		Username string `json:"username"` // Admin username for authentication
		Password string `json:"password"` // Admin password for authentication
	}

	// Parse and validate JSON request body
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	slog.Info("Login attempt", "username", req.Username)

	// Delegate authentication to service layer for business logic
	token, err := h.authService.Login(r.Context(), req.Username, req.Password)
	if err != nil {
		slog.Error("Operation failed", "operation", "admin login failed", "error", err.Error())
		// Record failed attempt for rate limiting
		h.rateLimiter.RecordFailedAttempt(clientIP)
		// Generic error message prevents username enumeration attacks
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
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
		slog.Error("Operation failed", "operation", "encode login response", "error", err.Error())
	}
}

// Logout handles admin session termination via HTTP POST.
// Invalidates session tokens and clears authentication cookies.
// Redirects users back to main page for seamless user experience.
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
	// Attempt to retrieve existing session token from cookie
	cookie, err := r.Cookie("admin_token")
	if err == nil {
		// Invalidate session token on server side for security
		if logoutErr := h.authService.Logout(r.Context(), cookie.Value); logoutErr != nil {
			slog.Error("Operation failed", "operation", "logout session", "error", logoutErr.Error())
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

	// Return logout confirmation in JSON format
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "logged out"}); err != nil {
		slog.Error("Operation failed", "operation", "encode logout response", "error", err.Error())
	}
}

// AdminPage serves the admin dashboard HTML page for authenticated administrators.
// This endpoint is protected by RequireAuth middleware and provides branch management interface.
// Serves static HTML file with embedded JavaScript for dynamic admin operations.
//
// Request: GET /admin/ (requires valid admin_token cookie)
//
// Response:
//   - 200 OK: Admin dashboard HTML page
//   - 302 Redirect: To main page if not authenticated (handled by middleware)
//
// Security:
//   - Protected by RequireAuth middleware
//   - Requires valid session token
//   - Redirects to main page if unauthorized
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
//   - 302 Redirect: To main page if not authenticated (handled by middleware)
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
//   - 302 Redirect: To main page if not authenticated (handled by middleware)
func (h *AdminHandler) AdminBookingsPage(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "./web/static/admin-bookings.html")
}
