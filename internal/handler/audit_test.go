package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/GringottsBankBranchBooking/internal/domain"
	"github.com/GringottsBankBranchBooking/internal/repository/memory"
	"github.com/GringottsBankBranchBooking/internal/service"
)

const (
	seededBranchID  = "550e8400-e29b-41d4-a716-446655440001"
	seededServiceID = "660e8400-e29b-41d4-a716-446655440001"
)

// assertAuditEntry fails the test unless exactly one audit entry was recorded
// and it matches want (ID and timestamp are assigned by the repository).
func assertAuditEntry(t *testing.T, repo *memory.AuditRepository, want domain.AuditEntry) {
	t.Helper()

	entries := repo.Entries()
	if len(entries) != 1 {
		t.Fatalf("Expected exactly 1 audit entry, got %d: %+v", len(entries), entries)
	}
	got := entries[0]
	if got.AdminID != want.AdminID || got.Action != want.Action ||
		got.TargetType != want.TargetType || got.TargetID != want.TargetID || got.Details != want.Details {
		t.Errorf("Audit entry mismatch:\n got  %+v\n want %+v", got, want)
	}
	if got.CreatedAt.IsZero() {
		t.Error("Expected audit entry to carry a timestamp")
	}
}

func assertNoAuditEntries(t *testing.T, repo *memory.AuditRepository) {
	t.Helper()

	if entries := repo.Entries(); len(entries) != 0 {
		t.Errorf("Expected no audit entries, got %+v", entries)
	}
}

func newCancelFixture() (http.Handler, *memory.BookingRepository, *memory.AuditRepository) {
	bookingRepo := memory.NewBookingRepository("partial")
	auditRepo := memory.NewAuditRepository()
	bookingHandler := NewBookingHandler(
		service.NewBookingService(bookingRepo, memory.NewBranchRepository()),
		service.NewAuditService(auditRepo),
	)

	r := chi.NewRouter()
	r.Delete("/api/bookings/{id}", bookingHandler.CancelBooking)
	return r, bookingRepo, auditRepo
}

func TestCancelBooking_RecordsAdminInAuditTrail(t *testing.T) {
	router, _, auditRepo := newCancelFixture()

	w := httptest.NewRecorder()
	router.ServeHTTP(w, withAdmin(httptest.NewRequest("DELETE", "/api/bookings/1", nil)))

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d (body: %q)", http.StatusOK, w.Code, w.Body.String())
	}
	assertAuditEntry(t, auditRepo, domain.AuditEntry{
		AdminID:    testAdminID,
		Action:     domain.AuditActionBookingCancel,
		TargetType: domain.AuditTargetBooking,
		TargetID:   "1",
	})
}

func TestCancelBooking_NotFoundIsNotAudited(t *testing.T) {
	router, _, auditRepo := newCancelFixture()

	w := httptest.NewRecorder()
	router.ServeHTTP(w, withAdmin(httptest.NewRequest("DELETE", "/api/bookings/does-not-exist", nil)))

	if w.Code != http.StatusNotFound {
		t.Fatalf("Expected status %d, got %d", http.StatusNotFound, w.Code)
	}
	assertNoAuditEntries(t, auditRepo)
}

// A privileged handler reached without an authenticated identity must refuse
// to act at all, rather than perform an action it cannot attribute.
func TestCancelBooking_RefusesWithoutAdminIdentity(t *testing.T) {
	router, bookingRepo, auditRepo := newCancelFixture()

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("DELETE", "/api/bookings/1", nil))

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
	assertNoAuditEntries(t, auditRepo)

	bookings, err := bookingRepo.GetAll(httptest.NewRequest("GET", "/", nil).Context())
	if err != nil {
		t.Fatalf("GetAll failed: %v", err)
	}
	for _, booking := range bookings {
		if booking.ID == "1" && booking.Status == string(domain.StatusCancelled) {
			t.Error("Booking must not be cancelled by an unauthenticated request")
		}
	}
}

func newBranchAuditFixture() (*BranchHandler, *memory.BranchRepository, *memory.AuditRepository) {
	branchRepo := memory.NewBranchRepository()
	auditRepo := memory.NewAuditRepository()
	branchHandler := NewBranchHandler(service.NewBranchService(branchRepo), service.NewAuditService(auditRepo))
	return branchHandler, branchRepo, auditRepo
}

const validBranchBody = `{"name":"Audit Branch","city":"Cape Town","address":"1 Long St","phone":"021 555 0100","capacity":10,"services":["` + seededServiceID + `"]}`

func TestCreateBranch_RecordsAdminInAuditTrail(t *testing.T) {
	branchHandler, _, auditRepo := newBranchAuditFixture()

	w := httptest.NewRecorder()
	branchHandler.CreateBranch(w, withAdmin(httptest.NewRequest("POST", "/admin/branches", bytes.NewBufferString(validBranchBody))))

	if w.Code != http.StatusCreated {
		t.Fatalf("Expected status %d, got %d (body: %q)", http.StatusCreated, w.Code, w.Body.String())
	}
	// The in-memory repository derives the ID from the name.
	assertAuditEntry(t, auditRepo, domain.AuditEntry{
		AdminID:    testAdminID,
		Action:     domain.AuditActionBranchCreate,
		TargetType: domain.AuditTargetBranch,
		TargetID:   "branch-Audit Branch",
		Details:    "name=Audit Branch",
	})
}

func TestCreateBranch_InvalidInputIsNotAudited(t *testing.T) {
	branchHandler, _, auditRepo := newBranchAuditFixture()

	w := httptest.NewRecorder()
	branchHandler.CreateBranch(w, withAdmin(httptest.NewRequest("POST", "/admin/branches", bytes.NewBufferString(`{"name":""}`))))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
	assertNoAuditEntries(t, auditRepo)
}

func TestCreateBranch_RefusesWithoutAdminIdentity(t *testing.T) {
	branchHandler, branchRepo, auditRepo := newBranchAuditFixture()

	w := httptest.NewRecorder()
	branchHandler.CreateBranch(w, httptest.NewRequest("POST", "/admin/branches", bytes.NewBufferString(validBranchBody)))

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
	assertNoAuditEntries(t, auditRepo)

	branches, _ := branchRepo.GetAll(httptest.NewRequest("GET", "/", nil).Context())
	if len(branches) != 3 {
		t.Errorf("Expected only the 3 seeded branches, got %d", len(branches))
	}
}

func TestDeleteBranch_RecordsAdminAndImpactInAuditTrail(t *testing.T) {
	branchHandler, _, auditRepo := newBranchAuditFixture()

	w := httptest.NewRecorder()
	newBranchRouter(branchHandler).ServeHTTP(w, withAdmin(httptest.NewRequest("DELETE", "/admin/branches/"+seededBranchID, nil)))

	if w.Code != http.StatusNoContent {
		t.Fatalf("Expected status %d, got %d", http.StatusNoContent, w.Code)
	}
	assertAuditEntry(t, auditRepo, domain.AuditEntry{
		AdminID:    testAdminID,
		Action:     domain.AuditActionBranchDelete,
		TargetType: domain.AuditTargetBranch,
		TargetID:   seededBranchID,
		Details:    "bookings_removed=0",
	})
}

func TestDeleteBranch_NotFoundIsNotAudited(t *testing.T) {
	branchHandler, _, auditRepo := newBranchAuditFixture()

	w := httptest.NewRecorder()
	newBranchRouter(branchHandler).ServeHTTP(w, withAdmin(httptest.NewRequest("DELETE", "/admin/branches/does-not-exist", nil)))

	if w.Code != http.StatusNotFound {
		t.Fatalf("Expected status %d, got %d", http.StatusNotFound, w.Code)
	}
	assertNoAuditEntries(t, auditRepo)
}

func TestDeleteBranch_RefusesWithoutAdminIdentity(t *testing.T) {
	branchHandler, branchRepo, auditRepo := newBranchAuditFixture()

	w := httptest.NewRecorder()
	newBranchRouter(branchHandler).ServeHTTP(w, httptest.NewRequest("DELETE", "/admin/branches/"+seededBranchID, nil))

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
	assertNoAuditEntries(t, auditRepo)

	if _, err := branchRepo.GetByID(httptest.NewRequest("GET", "/", nil).Context(), seededBranchID); err != nil {
		t.Errorf("Branch must survive an unauthenticated delete: %v", err)
	}
}
