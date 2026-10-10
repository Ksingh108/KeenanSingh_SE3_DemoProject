package middleware

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/GringottsBankBranchBooking/internal/service"
)

// RequireAuth creates a middleware that protects routes by validating admin session tokens.
// This middleware checks for a valid admin_token cookie and either calls the next handler
// (on success) or returns a 401 Unauthorized response (on failure).
// Used to gate sensitive API endpoints and admin pages.
//
// Security Features:
// - Server-side token validation prevents token tampering
// - HTTP-only cookie requirement protects against XSS
// - Generic error responses prevent information leakage
//
// Parameters:
//   - authService: Service to validate authentication tokens
//
// Returns:
//   - Middleware function that validates auth before proceeding
func RequireAuth(authService *service.AuthService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Attempt to retrieve admin session token from cookie
			cookie, err := r.Cookie("admin_token")

			// Check if cookie exists
			if err != nil {
				// Return 401 for API endpoints (JSON response)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				if err := json.NewEncoder(w).Encode(map[string]string{"error": "Unauthorized"}); err != nil {
					// Silently fail if encoding fails; connection already closed
				}
				return
			}

			// Validate token is valid on server side
			adminID, valid := authService.ValidateToken(r.Context(), cookie.Value)
			if !valid {
				// Return 401 for API endpoints (JSON response)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				if err := json.NewEncoder(w).Encode(map[string]string{"error": "Unauthorized"}); err != nil {
					// Silently fail if encoding fails; connection already closed
				}
				return
			}

			// Authentication successful: expose the admin's identity to the
			// protected handler so privileged actions can be attributed.
			next.ServeHTTP(w, r.WithContext(WithAdminID(r.Context(), adminID)))
		})
	}
}

// adminIDKey is the context key for the authenticated admin's ID. A private
// type prevents collisions with keys defined in other packages.
type adminIDKey struct{}

// WithAdminID returns a copy of ctx carrying the authenticated admin's ID.
// RequireAuth calls it after validating the session; tests use it to stand in
// for an authenticated request.
func WithAdminID(ctx context.Context, adminID string) context.Context {
	return context.WithValue(ctx, adminIDKey{}, adminID)
}

// AdminIDFromContext returns the authenticated admin's ID set by RequireAuth.
// The boolean is false when the request did not pass through authentication.
func AdminIDFromContext(ctx context.Context) (string, bool) {
	adminID, ok := ctx.Value(adminIDKey{}).(string)
	return adminID, ok && adminID != ""
}
