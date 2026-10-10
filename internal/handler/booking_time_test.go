package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/GringottsBankBranchBooking/internal/repository/memory"
	"github.com/GringottsBankBranchBooking/internal/service"
	"github.com/GringottsBankBranchBooking/pkg/clock"
)

// fixedNow is 10:30 on 7 October 2026 in Johannesburg (UTC+2).
var fixedNow = time.Date(2026, 10, 7, 10, 30, 0, 0, time.FixedZone("SAST", 2*60*60))

func newBookingFixtureAt(now time.Time) (*BookingHandler, *memory.BookingRepository) {
	bookingRepo := memory.NewBookingRepository("empty")
	svc := service.NewBookingService(bookingRepo, memory.NewBranchRepository())
	return NewBookingHandler(svc, newTestAuditService(), WithClock(clock.Fixed(now))), bookingRepo
}

func bookingBody(date, slot string) string {
	return `{"branch_id":"` + seededBranchID + `","service_id":"` + seededServiceID + `",` +
		`"customer_name":"Jane Smith","email":"jane@example.com","phone":"+27 21 555 0100",` +
		`"date":"` + date + `","time_slot":"` + slot + `"}`
}

func TestBookingHandler_CreateBooking_RespectsCurrentTime(t *testing.T) {
	tests := []struct {
		name         string
		now          time.Time
		date         string
		slot         string
		expectedCode int
		expectedMsg  string
	}{
		{"earlier slot today", fixedNow, "2026-10-07", "09:00", http.StatusBadRequest, "Time slot is in the past"},
		{"slot starting exactly now", fixedNow, "2026-10-07", "10:30", http.StatusBadRequest, "Time slot is in the past"},
		{"later slot today", fixedNow, "2026-10-07", "14:00", http.StatusOK, ""},
		{"slot tomorrow", fixedNow, "2026-10-08", "09:00", http.StatusOK, ""},
		{"yesterday", fixedNow, "2026-10-06", "16:00", http.StatusBadRequest, "date in the past"},
		// 01:00 on the 7th in Johannesburg is still the 6th in UTC; the 6th is already past locally.
		{"yesterday locally but today in UTC", time.Date(2026, 10, 7, 1, 0, 0, 0, time.FixedZone("SAST", 2*60*60)), "2026-10-06", "16:00", http.StatusBadRequest, "date in the past"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, bookingRepo := newBookingFixtureAt(tt.now)

			req := httptest.NewRequest("POST", "/api/bookings", bytes.NewBufferString(bookingBody(tt.date, tt.slot)))
			w := httptest.NewRecorder()
			handler.CreateBooking(w, req)

			if w.Code != tt.expectedCode {
				t.Fatalf("Expected status %d, got %d (body: %q)", tt.expectedCode, w.Code, w.Body.String())
			}
			if tt.expectedMsg != "" && !strings.Contains(w.Body.String(), tt.expectedMsg) {
				t.Errorf("Expected body to mention %q, got %q", tt.expectedMsg, w.Body.String())
			}

			bookings, err := bookingRepo.GetAll(req.Context())
			if err != nil {
				t.Fatalf("GetAll failed: %v", err)
			}
			wantStored := 0
			if tt.expectedCode == http.StatusOK {
				wantStored = 1
			}
			if len(bookings) != wantStored {
				t.Errorf("Expected %d stored bookings, got %d", wantStored, len(bookings))
			}
		})
	}
}

func TestBookingHandler_CheckAvailability_RespectsCurrentDate(t *testing.T) {
	handler, _ := newBookingFixtureAt(fixedNow)
	r := chi.NewRouter()
	r.Get("/availability/{branchId}/{date}", handler.CheckAvailability)

	tests := []struct {
		name         string
		date         string
		expectedCode int
	}{
		{"today", "2026-10-07", http.StatusOK},
		{"tomorrow", "2026-10-08", http.StatusOK},
		{"yesterday", "2026-10-06", http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/availability/"+seededBranchID+"/"+tt.date, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.expectedCode {
				t.Errorf("Expected status %d, got %d", tt.expectedCode, w.Code)
			}
		})
	}
}
