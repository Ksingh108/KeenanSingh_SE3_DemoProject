package handler

import (
	"net/http"

	"github.com/gringottsbankbranchbooking/internal/apierror"
	"github.com/gringottsbankbranchbooking/internal/middleware"
)

// requireAdminID returns the authenticated admin's ID from the request context.
// Privileged handlers sit behind middleware.RequireAuth, which sets it; if it is
// missing the route was wired without authentication, so the handler refuses to
// act rather than perform a privileged action it could not attribute to anyone.
func requireAdminID(w http.ResponseWriter, r *http.Request) (string, bool) {
	adminID, ok := middleware.AdminIDFromContext(r.Context())
	if !ok {
		apierror.Write(w, http.StatusUnauthorized, "Unauthorized")
		return "", false
	}
	return adminID, true
}
