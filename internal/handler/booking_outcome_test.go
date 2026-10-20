package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gringottsbankbranchbooking/internal/domain"
	"github.com/gringottsbankbranchbooking/internal/middleware"
	"github.com/gringottsbankbranchbooking/internal/repository/memory"
	"github.com/gringottsbankbranchbooking/internal/service"
)

type outcomeRecorder struct{ outcomes []string }

func (o *outcomeRecorder) ObserveRequest(_, _ string, _ int, _ time.Duration, outcome string) {
	o.outcomes = append(o.outcomes, outcome)
}

// The metrics label for a 409 cannot be derived from the status alone, so the
// handler must report which conflict occurred.
func TestBookingHandler_CreateBooking_ReportsConflictOutcome(t *testing.T) {
	bookingRepo := memory.NewBookingRepository("empty")
	bookingHandler := NewBookingHandler(service.NewBookingService(bookingRepo, memory.NewBranchRepository()), nil)
	recorder := &outcomeRecorder{}
	router := chi.NewRouter()
	router.Use(middleware.RequestObservability(slog.Default(), recorder))
	router.Post("/api/bookings", bookingHandler.CreateBooking)

	booking := domain.Booking{
		BranchID:     "550e8400-e29b-41d4-a716-446655440001",
		ServiceID:    "660e8400-e29b-41d4-a716-446655440001",
		CustomerName: "Ron Weasley",
		Email:        "ron@example.com",
		Phone:        "+27 82 123 4567",
		Date:         time.Now().AddDate(0, 0, 30).Format("2006-01-02"),
		TimeSlot:     "13:00",
	}
	send := func(b domain.Booking) *httptest.ResponseRecorder {
		body, err := json.Marshal(b)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/bookings", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "outcome-key-789")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	created := send(booking)
	if created.Code != http.StatusCreated {
		t.Fatalf("first status = %d, want 201: %s", created.Code, created.Body)
	}
	var stored domain.Booking
	if err := json.Unmarshal(created.Body.Bytes(), &stored); err != nil {
		t.Fatal(err)
	}

	changed := booking
	changed.CustomerName = "William Weasley"
	if w := send(changed); w.Code != http.StatusConflict {
		t.Fatalf("changed request status = %d, want 409", w.Code)
	}
	if err := bookingRepo.Cancel(context.Background(), stored.ID); err != nil {
		t.Fatal(err)
	}
	if w := send(booking); w.Code != http.StatusConflict {
		t.Fatalf("cancelled retry status = %d, want 409", w.Code)
	}

	want := []string{"", "idempotency_key_reused", "booking_cancelled"}
	if len(recorder.outcomes) != len(want) {
		t.Fatalf("outcomes = %q, want %q", recorder.outcomes, want)
	}
	for i := range want {
		if recorder.outcomes[i] != want[i] {
			t.Fatalf("outcomes = %q, want %q", recorder.outcomes, want)
		}
	}
}
