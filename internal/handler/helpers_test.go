package handler

import (
	"net/http"

	"github.com/GringottsBankBranchBooking/internal/middleware"
	"github.com/GringottsBankBranchBooking/internal/repository/memory"
	"github.com/GringottsBankBranchBooking/internal/service"
)

// testAdminID is the admin identity attached to requests built with withAdmin.
const testAdminID = "admin1"

// newTestAuditService returns an AuditService backed by a throwaway in-memory
// repository, for tests that do not inspect the audit trail.
func newTestAuditService() *service.AuditService {
	return service.NewAuditService(memory.NewAuditRepository())
}

// withAdmin marks the request as authenticated by testAdminID, standing in for
// middleware.RequireAuth when a handler is called directly.
func withAdmin(r *http.Request) *http.Request {
	return r.WithContext(middleware.WithAdminID(r.Context(), testAdminID))
}
