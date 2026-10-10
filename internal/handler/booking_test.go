package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/GringottsBankBranchBooking/internal/domain"
	"github.com/GringottsBankBranchBooking/internal/repository/memory"
	"github.com/GringottsBankBranchBooking/internal/service"
	"github.com/go-chi/chi/v5"
)

func TestBookingHandler_CreateBooking_InvalidJSON(t *testing.T) {
	bookingRepo := memory.NewBookingRepository("empty")
	branchRepo := memory.NewBranchRepository()
	svc := service.NewBookingService(bookingRepo, branchRepo)
	handler := NewBookingHandler(svc, newTestAuditService())

	req := httptest.NewRequest("POST", "/bookings", bytes.NewBufferString("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.CreateBooking(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestBookingHandler_GetBookingsByEmail_InvalidEmail(t *testing.T) {
	bookingRepo := memory.NewBookingRepository("empty")
	branchRepo := memory.NewBranchRepository()
	svc := service.NewBookingService(bookingRepo, branchRepo)
	handler := NewBookingHandler(svc, newTestAuditService())

	r := chi.NewRouter()
	r.Get("/bookings/{email}", handler.GetBookingsByEmail)

	req := httptest.NewRequest("GET", "/bookings/invalid-email", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestBookingHandler_CheckAvailability_InvalidParams(t *testing.T) {
	bookingRepo := memory.NewBookingRepository("empty")
	branchRepo := memory.NewBranchRepository()
	svc := service.NewBookingService(bookingRepo, branchRepo)
	handler := NewBookingHandler(svc, newTestAuditService())

	r := chi.NewRouter()
	r.Get("/availability/{branchId}/{date}", handler.CheckAvailability)

	tests := []struct {
		name     string
		branchId string
		date     string
	}{
		{"invalid branch", "invalid@branch", "2025-12-31"},
		{"invalid date", "valid-branch", "invalid-date"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/availability/"+tt.branchId+"/"+tt.date, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
			}
		})
	}
}

func TestBookingHandler_GetBookings(t *testing.T) {
	bookingRepo := memory.NewBookingRepository("partial")
	branchRepo := memory.NewBranchRepository()
	svc := service.NewBookingService(bookingRepo, branchRepo)
	handler := NewBookingHandler(svc, newTestAuditService())

	req := httptest.NewRequest("GET", "/api/bookings", nil)
	w := httptest.NewRecorder()

	handler.GetBookings(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestBookingHandler_CheckAvailability_Success(t *testing.T) {
	bookingRepo := memory.NewBookingRepository("partial")
	branchRepo := memory.NewBranchRepository()
	svc := service.NewBookingService(bookingRepo, branchRepo)
	handler := NewBookingHandler(svc, newTestAuditService())

	r := chi.NewRouter()
	r.Get("/availability/{branchId}/{date}", handler.CheckAvailability)

	futureDate := time.Now().AddDate(0, 0, 30).Format("2006-01-02")
	req := httptest.NewRequest("GET", "/availability/550e8400-e29b-41d4-a716-446655440001/"+futureDate, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestBookingHandler_GetBookings_Pagination_InvalidLimit(t *testing.T) {
	bookingRepo := memory.NewBookingRepository("partial")
	branchRepo := memory.NewBranchRepository()
	svc := service.NewBookingService(bookingRepo, branchRepo)
	handler := NewBookingHandler(svc, newTestAuditService())

	tests := []struct {
		name  string
		limit string
	}{
		{"limit too high", "2000"},
		{"limit zero", "0"},
		{"limit negative", "-1"},
		{"limit non-numeric", "abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/bookings?limit="+tt.limit, nil)
			w := httptest.NewRecorder()
			handler.GetBookings(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
			}
		})
	}
}

func TestBookingHandler_GetBookings_Pagination_InvalidOffset(t *testing.T) {
	bookingRepo := memory.NewBookingRepository("partial")
	branchRepo := memory.NewBranchRepository()
	svc := service.NewBookingService(bookingRepo, branchRepo)
	handler := NewBookingHandler(svc, newTestAuditService())

	tests := []struct {
		name   string
		offset string
	}{
		{"offset negative", "-1"},
		{"offset non-numeric", "abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/bookings?offset="+tt.offset, nil)
			w := httptest.NewRecorder()
			handler.GetBookings(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
			}
		})
	}
}

func TestBookingHandler_GetBookings_Pagination_FirstPage(t *testing.T) {
	bookingRepo := memory.NewBookingRepository("full")
	branchRepo := memory.NewBranchRepository()
	svc := service.NewBookingService(bookingRepo, branchRepo)
	handler := NewBookingHandler(svc, newTestAuditService())

	req := httptest.NewRequest("GET", "/api/bookings?limit=3&offset=0", nil)
	w := httptest.NewRecorder()
	handler.GetBookings(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	var bookings []*domain.Booking
	if err := json.NewDecoder(w.Body).Decode(&bookings); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(bookings) != 3 {
		t.Errorf("Expected 3 bookings, got %d", len(bookings))
	}
}

func TestBookingHandler_GetBookings_Pagination_SecondPage(t *testing.T) {
	bookingRepo := memory.NewBookingRepository("full")
	branchRepo := memory.NewBranchRepository()
	svc := service.NewBookingService(bookingRepo, branchRepo)
	handler := NewBookingHandler(svc, newTestAuditService())

	// Get first page
	req1 := httptest.NewRequest("GET", "/api/bookings?limit=3&offset=0", nil)
	w1 := httptest.NewRecorder()
	handler.GetBookings(w1, req1)

	var firstPage []*domain.Booking
	if err := json.NewDecoder(w1.Body).Decode(&firstPage); err != nil {
		t.Fatalf("Failed to decode first page: %v", err)
	}

	// Get second page
	req2 := httptest.NewRequest("GET", "/api/bookings?limit=3&offset=3", nil)
	w2 := httptest.NewRecorder()
	handler.GetBookings(w2, req2)

	var secondPage []*domain.Booking
	if err := json.NewDecoder(w2.Body).Decode(&secondPage); err != nil {
		t.Fatalf("Failed to decode second page: %v", err)
	}

	if len(secondPage) != 3 {
		t.Errorf("Expected 3 bookings in second page, got %d", len(secondPage))
	}

	// Verify no duplicates between pages
	firstPageIDs := make(map[string]bool)
	for _, b := range firstPage {
		firstPageIDs[b.ID] = true
	}

	for _, b := range secondPage {
		if firstPageIDs[b.ID] {
			t.Errorf("Duplicate booking ID found: %s", b.ID)
		}
	}
}

func TestBookingHandler_GetBookings_Pagination_DefaultLimit(t *testing.T) {
	bookingRepo := memory.NewBookingRepository("empty")
	branchRepo := memory.NewBranchRepository()
	svc := service.NewBookingService(bookingRepo, branchRepo)
	handler := NewBookingHandler(svc, newTestAuditService())

	req := httptest.NewRequest("GET", "/api/bookings", nil)
	w := httptest.NewRecorder()
	handler.GetBookings(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestBookingHandler_GetBookingsByEmail_Pagination_FirstPage(t *testing.T) {
	bookingRepo := memory.NewBookingRepository("full")
	branchRepo := memory.NewBranchRepository()
	svc := service.NewBookingService(bookingRepo, branchRepo)
	handler := NewBookingHandler(svc, newTestAuditService())

	r := chi.NewRouter()
	r.Get("/bookings/{email}", handler.GetBookingsByEmail)

	req := httptest.NewRequest("GET", "/bookings/customer1@example.com?limit=1&offset=0", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestBookingHandler_GetBookingsByEmail_Pagination_InvalidLimit(t *testing.T) {
	bookingRepo := memory.NewBookingRepository("partial")
	branchRepo := memory.NewBranchRepository()
	svc := service.NewBookingService(bookingRepo, branchRepo)
	handler := NewBookingHandler(svc, newTestAuditService())

	r := chi.NewRouter()
	r.Get("/bookings/{email}", handler.GetBookingsByEmail)

	req := httptest.NewRequest("GET", "/bookings/harry@hogwarts.edu?limit=2000", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}
