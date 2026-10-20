package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gringottsbankbranchbooking/internal/middleware"
	"github.com/gringottsbankbranchbooking/internal/repository/memory"
	"github.com/gringottsbankbranchbooking/internal/service"
)

func assertJSONError(t *testing.T, response *httptest.ResponseRecorder, statusCode int, expectedCode ...string) {
	t.Helper()
	if response.Code != statusCode {
		t.Fatalf("status = %d, want %d (body: %q)", response.Code, statusCode, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", contentType)
	}
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body is not JSON: %v", err)
	}
	if body.Error.Code == "" || body.Error.Message == "" {
		t.Fatalf("response body has no error code or message: %+v", body)
	}
	if len(expectedCode) > 0 && body.Error.Code != expectedCode[0] {
		t.Fatalf("error code = %q, want %q", body.Error.Code, expectedCode[0])
	}
}

// testAdminID is the admin identity attached to requests built with withAdmin.
const testAdminID = "admin1"

// newTestAdminActionService returns an admin action service backed by in-memory
// repositories for tests that do not inspect the audit trail.
func newTestAdminActionService() *service.AdminActionService {
	return service.NewAdminActionService(memory.NewAdminActionRepository(
		memory.NewBookingRepository("empty"),
		memory.NewBranchRepository(),
		memory.NewAuditRepository(),
	))
}

func newTestAdminActions(bookings *memory.BookingRepository, branches *memory.BranchRepository) *service.AdminActionService {
	return service.NewAdminActionService(memory.NewAdminActionRepository(bookings, branches, memory.NewAuditRepository()))
}

// withAdmin marks the request as authenticated by testAdminID, standing in for
// middleware.RequireAuth when a handler is called directly.
func withAdmin(r *http.Request) *http.Request {
	return r.WithContext(middleware.WithAdminID(r.Context(), testAdminID))
}
