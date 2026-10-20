package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gringottsbankbranchbooking/internal/domain"
	"github.com/gringottsbankbranchbooking/internal/middleware"
	"github.com/gringottsbankbranchbooking/internal/repository/memory"
	"github.com/gringottsbankbranchbooking/internal/service"
	"github.com/gringottsbankbranchbooking/pkg/clock"
)

func TestBookingHandler_CreateBooking_BodyLimit(t *testing.T) {
	for _, test := range []struct {
		name   string
		body   string
		status int
	}{
		{"at limit", "{}" + strings.Repeat(" ", maxBookingBodyBytes-2), http.StatusBadRequest},
		{"oversized field", `{"customer_name":"` + strings.Repeat("a", maxBookingBodyBytes) + `"}`, http.StatusRequestEntityTooLarge},
		{"oversized trailing whitespace", "{}" + strings.Repeat(" ", maxBookingBodyBytes), http.StatusRequestEntityTooLarge},
		{"multiple JSON values", "{} {}", http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := NewBookingHandler(nil, nil)
			if test.status != http.StatusRequestEntityTooLarge {
				handler = NewBookingHandler(service.NewBookingService(memory.NewBookingRepository("empty"), memory.NewBranchRepository()), nil)
			}
			req := httptest.NewRequest(http.MethodPost, "/api/bookings", strings.NewReader(test.body))
			req.Header.Set("Content-Type", "application/json")
			req.ContentLength = -1
			response := httptest.NewRecorder()
			handler.CreateBooking(response, req)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.status, response.Body.String())
			}
		})
	}
}

func TestBookingHandler_CreateBooking_RateLimit(t *testing.T) {
	handler := NewBookingHandler(nil, nil)
	for attempt := 0; attempt <= bookingRequestsPerMinute; attempt++ {
		req := httptest.NewRequest(http.MethodPost, "/api/bookings", strings.NewReader("invalid json"))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "198.51.100.7:1234"
		req.Header.Set("X-Forwarded-For", "203.0.113."+strconv.Itoa(attempt))
		req.Header.Set("X-Real-IP", "203.0.113."+strconv.Itoa(attempt))
		response := httptest.NewRecorder()
		handler.CreateBooking(response, req)
		want := http.StatusBadRequest
		if attempt == bookingRequestsPerMinute {
			want = http.StatusTooManyRequests
			retryAfter, err := strconv.Atoi(response.Header().Get("Retry-After"))
			if err != nil || retryAfter < 1 || retryAfter > 60 {
				t.Fatalf("invalid Retry-After: %q", response.Header().Get("Retry-After"))
			}
		}
		if response.Code != want {
			t.Fatalf("attempt %d: status = %d, want %d", attempt, response.Code, want)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/api/bookings", strings.NewReader("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "198.51.100.8:1234"
	response := httptest.NewRecorder()
	handler.CreateBooking(response, req)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("another IP: status = %d, want 400", response.Code)
	}
}

func TestBookingHandler_CreateBooking_InvalidJSON(t *testing.T) {
	bookingRepo := memory.NewBookingRepository("empty")
	branchRepo := memory.NewBranchRepository()
	svc := service.NewBookingService(bookingRepo, branchRepo)
	handler := NewBookingHandler(svc, newTestAdminActionService())

	req := httptest.NewRequest("POST", "/bookings", bytes.NewBufferString("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.CreateBooking(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestBookingHandler_CreateBooking_RequiresJSONContentType(t *testing.T) {
	handler := NewBookingHandler(nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/bookings", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "text/plain")
	response := httptest.NewRecorder()

	handler.CreateBooking(response, req)

	assertJSONError(t, response, http.StatusUnsupportedMediaType)
}

func TestBookingHandler_CreateBooking_InvalidPhoneFormatMessage(t *testing.T) {
	bookingRepo := memory.NewBookingRepository("empty")
	branchRepo := memory.NewBranchRepository()
	svc := service.NewBookingService(bookingRepo, branchRepo)
	handler := NewBookingHandler(svc, newTestAdminActionService())

	booking := domain.Booking{
		BranchID:     "550e8400-e29b-41d4-a716-446655440001",
		ServiceID:    "660e8400-e29b-41d4-a716-446655440001",
		CustomerName: "Ron Weasley",
		Email:        "ron@example.com",
		Phone:        "abc",
		Date:         time.Now().AddDate(0, 0, 30).Format("2006-01-02"),
		TimeSlot:     "11:00",
	}
	body, err := json.Marshal(booking)
	if err != nil {
		t.Fatalf("Failed to encode booking request: %v", err)
	}

	req := httptest.NewRequest("POST", "/api/bookings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.CreateBooking(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("Expected status %d, got %d: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "South African") || !strings.Contains(w.Body.String(), "+27") {
		t.Fatalf("Expected invalid phone message to include South African guidance and an example, got %s", w.Body.String())
	}
}

func TestBookingHandler_CreateBooking_ReturnsPersistedBooking(t *testing.T) {
	var logs bytes.Buffer
	previousLogger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previousLogger) })
	slog.SetDefault(slog.New(middleware.NewContextHandler(slog.NewJSONHandler(&logs, nil))))
	bookingRepo := memory.NewBookingRepository("empty")
	branchRepo := memory.NewBranchRepository()
	svc := service.NewBookingService(bookingRepo, branchRepo)
	handler := NewBookingHandler(svc, newTestAdminActionService())

	booking := domain.Booking{
		BranchID:     "550e8400-e29b-41d4-a716-446655440001",
		ServiceID:    "660e8400-e29b-41d4-a716-446655440001",
		CustomerName: "Ron Weasley",
		Email:        "ron@example.com",
		Phone:        "+27 82 123 4567",
		Date:         time.Now().AddDate(0, 0, 30).Format("2006-01-02"),
		TimeSlot:     "11:00",
	}
	body, err := json.Marshal(booking)
	if err != nil {
		t.Fatalf("Failed to encode booking request: %v", err)
	}

	req := httptest.NewRequest("POST", "/api/bookings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router := chi.NewRouter()
	router.Use(middleware.RequestObservability(slog.Default()))
	router.Post("/api/bookings", handler.CreateBooking)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Expected status %d, got %d: %s", http.StatusCreated, w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Expected application/json content type, got %q", got)
	}

	var created domain.Booking
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("Failed to decode created booking: %v", err)
	}
	if created.ID == "" || created.Status != string(domain.StatusConfirmed) {
		t.Errorf("Expected a persisted confirmed booking, got ID %q and status %q", created.ID, created.Status)
	}
	if created.CustomerName != booking.CustomerName || created.BranchID != booking.BranchID || created.ServiceID != booking.ServiceID || created.Date != booking.Date || created.TimeSlot != booking.TimeSlot {
		t.Errorf("Response booking does not match the submitted booking: got %+v", created)
	}
	if created.CreatedAt.IsZero() {
		t.Error("Expected created booking to include its persistence timestamp")
	}
	foundCreateLog := false
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatal(err)
		}
		if strings.Count(line, `"request_id":`) != 1 || entry["request_id"] != w.Header().Get("X-Request-ID") {
			t.Fatalf("log correlation mismatch: %s", line)
		}
		if entry["msg"] == "Booking created successfully" {
			foundCreateLog = true
			if entry["booking_id"] != created.ID || entry["branch_id"] != created.BranchID || entry["date"] != created.Date || entry["time_slot"] != created.TimeSlot {
				t.Fatalf("booking log does not match persisted response: %s", line)
			}
		}
	}
	if !foundCreateLog || strings.Contains(logs.String(), booking.Email) || strings.Contains(logs.String(), booking.Phone) {
		t.Fatalf("missing booking log or leaked customer contact details: %s", logs.String())
	}

	for attempt := 1; attempt < bookingRequestsPerMinute; attempt++ {
		invalidRequest := httptest.NewRequest(http.MethodPost, "/api/bookings", strings.NewReader("invalid json"))
		invalidRequest.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.CreateBooking(response, invalidRequest)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("attempt %d: status = %d, want 400", attempt+1, response.Code)
		}
	}
	blockedRequest := httptest.NewRequest(http.MethodPost, "/api/bookings", bytes.NewReader(body))
	blockedResponse := httptest.NewRecorder()
	handler.CreateBooking(blockedResponse, blockedRequest)
	if blockedResponse.Code != http.StatusTooManyRequests {
		t.Fatalf("successful booking must count toward quota: status = %d, want 429", blockedResponse.Code)
	}

	stored, err := bookingRepo.GetByEmailPaginated(req.Context(), booking.Email, 10, 0)
	if err != nil {
		t.Fatalf("Failed to retrieve persisted booking: %v", err)
	}
	if len(stored) != 1 || stored[0].ID != created.ID {
		t.Fatalf("Expected response booking %q to be persisted, got %+v", created.ID, stored)
	}
}

func TestBookingHandler_CreateBooking_IdempotencyKeyReplaysBooking(t *testing.T) {
	bookingRepo := memory.NewBookingRepository("empty")
	handler := NewBookingHandler(service.NewBookingService(bookingRepo, memory.NewBranchRepository()), nil)
	booking := domain.Booking{
		BranchID:     "550e8400-e29b-41d4-a716-446655440001",
		ServiceID:    "660e8400-e29b-41d4-a716-446655440001",
		CustomerName: "Ron Weasley",
		Email:        "ron@example.com",
		Phone:        "+27 82 123 4567",
		Date:         time.Now().AddDate(0, 0, 30).Format("2006-01-02"),
		TimeSlot:     "11:00",
	}
	body, err := json.Marshal(booking)
	if err != nil {
		t.Fatal(err)
	}
	request := func(body []byte) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/bookings", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "booking-request-123")
		response := httptest.NewRecorder()
		handler.CreateBooking(response, req)
		return response
	}

	first := request(body)
	second := request(body)
	if first.Code != http.StatusCreated || second.Code != http.StatusCreated {
		t.Fatalf("statuses = %d and %d, want 201: %s / %s", first.Code, second.Code, first.Body, second.Body)
	}
	var firstBooking, replayedBooking domain.Booking
	if err := json.Unmarshal(first.Body.Bytes(), &firstBooking); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(second.Body.Bytes(), &replayedBooking); err != nil {
		t.Fatal(err)
	}
	if firstBooking.ID == "" || replayedBooking.ID != firstBooking.ID {
		t.Fatalf("replayed booking ID = %q, original ID = %q", replayedBooking.ID, firstBooking.ID)
	}
	stored, err := bookingRepo.GetByEmailPaginated(context.Background(), booking.Email, 10, 0)
	if err != nil || len(stored) != 1 {
		t.Fatalf("stored bookings = %d, error = %v; want one", len(stored), err)
	}

	booking.CustomerName = "William Weasley"
	differentBody, err := json.Marshal(booking)
	if err != nil {
		t.Fatal(err)
	}
	conflict := request(differentBody)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("different request status = %d, want 409: %s", conflict.Code, conflict.Body)
	}
	assertJSONError(t, conflict, http.StatusConflict, "idempotency_key_reused")
}

func TestBookingHandler_CreateBooking_RejectsInvalidIdempotencyKey(t *testing.T) {
	handler := NewBookingHandler(nil, nil)
	request := httptest.NewRequest(http.MethodPost, "/api/bookings", strings.NewReader(`{}`))
	request.Header.Set("Idempotency-Key", "contains whitespace")
	response := httptest.NewRecorder()
	handler.CreateBooking(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
}

func TestBookingHandler_CreateBooking_ServiceNotAvailableAtBranch(t *testing.T) {
	bookingRepo := memory.NewBookingRepository("empty")
	branchRepo := memory.NewBranchRepository()
	handler := NewBookingHandler(service.NewBookingService(bookingRepo, branchRepo), newTestAdminActionService())
	booking := domain.Booking{
		BranchID:     "550e8400-e29b-41d4-a716-446655440002",
		ServiceID:    "660e8400-e29b-41d4-a716-446655440001",
		CustomerName: "Ron Weasley",
		Email:        "ron@example.com",
		Phone:        "+27 82 123 4567",
		Date:         time.Now().AddDate(0, 0, 30).Format("2006-01-02"),
		TimeSlot:     "11:00",
	}
	body, err := json.Marshal(booking)
	if err != nil {
		t.Fatalf("Failed to encode booking request: %v", err)
	}

	req := httptest.NewRequest("POST", "/api/bookings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.CreateBooking(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("Expected status %d, got %d: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
	assertJSONError(t, w, http.StatusBadRequest, "service_not_offered")
}

func TestBookingHandler_CreateBooking_BranchNotFoundReturnsNotFound(t *testing.T) {
	bookingRepo := memory.NewBookingRepository("empty")
	branchRepo := memory.NewBranchRepository()
	handler := NewBookingHandler(service.NewBookingService(bookingRepo, branchRepo), newTestAdminActionService())
	booking := domain.Booking{
		BranchID:     "11111111-1111-4111-8111-111111111111",
		ServiceID:    "660e8400-e29b-41d4-a716-446655440001",
		CustomerName: "Ron Weasley",
		Email:        "ron@example.com",
		Phone:        "+27 82 123 4567",
		Date:         time.Now().AddDate(0, 0, 30).Format("2006-01-02"),
		TimeSlot:     "11:00",
	}
	body, err := json.Marshal(booking)
	if err != nil {
		t.Fatalf("Failed to encode booking request: %v", err)
	}

	req := httptest.NewRequest("POST", "/api/bookings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.CreateBooking(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("Expected status %d, got %d: %s", http.StatusNotFound, w.Code, w.Body.String())
	}
	assertJSONError(t, w, http.StatusNotFound, "branch_not_found")
}

func TestBookingHandler_CreateBooking_SlotFullCode(t *testing.T) {
	bookingRepo := memory.NewBookingRepository("empty", 1)
	branchRepo := memory.NewBranchRepository()
	date := time.Now().AddDate(0, 0, 30).Format("2006-01-02")
	existing := &domain.Booking{
		BranchID:  "550e8400-e29b-41d4-a716-446655440001",
		ServiceID: "660e8400-e29b-41d4-a716-446655440001",
		Date:      date,
		TimeSlot:  "11:00",
	}
	if err := bookingRepo.CreateWithCapacityCheck(context.Background(), existing); err != nil {
		t.Fatalf("seed full slot: %v", err)
	}
	handler := NewBookingHandler(service.NewBookingService(bookingRepo, branchRepo), newTestAdminActionService())
	booking := domain.Booking{
		BranchID:     existing.BranchID,
		ServiceID:    existing.ServiceID,
		CustomerName: "Ron Weasley",
		Email:        "ron@example.com",
		Phone:        "+27 82 123 4567",
		Date:         date,
		TimeSlot:     existing.TimeSlot,
	}
	body, err := json.Marshal(booking)
	if err != nil {
		t.Fatalf("encode booking request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/bookings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.CreateBooking(response, req)

	assertJSONError(t, response, http.StatusConflict, "slot_full")
}

func TestBookingHandler_CreateBooking_SlotInPastCode(t *testing.T) {
	now := time.Date(2030, time.January, 1, 10, 30, 0, 0, time.FixedZone("SAST", 2*60*60))
	bookingRepo := memory.NewBookingRepository("empty")
	branchRepo := memory.NewBranchRepository()
	handler := NewBookingHandler(
		service.NewBookingService(bookingRepo, branchRepo),
		newTestAdminActionService(),
		WithClock(clock.Fixed(now)),
	)
	booking := domain.Booking{
		BranchID:     "550e8400-e29b-41d4-a716-446655440001",
		ServiceID:    "660e8400-e29b-41d4-a716-446655440001",
		CustomerName: "Ron Weasley",
		Email:        "ron@example.com",
		Phone:        "+27 82 123 4567",
		Date:         "2030-01-01",
		TimeSlot:     "10:00",
	}
	body, err := json.Marshal(booking)
	if err != nil {
		t.Fatalf("encode booking request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/bookings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.CreateBooking(response, req)

	assertJSONError(t, response, http.StatusBadRequest, "slot_in_past")
}

func TestBookingHandler_CheckAvailability_BranchNotFoundReturnsNotFound(t *testing.T) {
	bookingRepo := memory.NewBookingRepository("empty")
	branchRepo := memory.NewBranchRepository()
	handler := NewBookingHandler(service.NewBookingService(bookingRepo, branchRepo), newTestAdminActionService())

	r := chi.NewRouter()
	r.Get("/availability/{branchId}/{date}", handler.CheckAvailability)

	req := httptest.NewRequest("GET", "/availability/11111111-1111-4111-8111-111111111111/2030-01-05", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("Expected status %d, got %d: %s", http.StatusNotFound, w.Code, w.Body.String())
	}
}

func TestBookingHandler_GetBookingsByEmail_InvalidEmail(t *testing.T) {
	bookingRepo := memory.NewBookingRepository("empty")
	branchRepo := memory.NewBranchRepository()
	svc := service.NewBookingService(bookingRepo, branchRepo)
	handler := NewBookingHandler(svc, newTestAdminActionService())

	r := chi.NewRouter()
	r.Get("/bookings", handler.GetBookings)

	req := httptest.NewRequest("GET", "/bookings?email=invalid-email", nil)
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
	handler := NewBookingHandler(svc, newTestAdminActionService())

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
	handler := NewBookingHandler(svc, newTestAdminActionService())

	req := httptest.NewRequest("GET", "/api/bookings", nil)
	w := httptest.NewRecorder()

	handler.GetBookings(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestBookingRoutes_RequireAdminAuth(t *testing.T) {
	bookingRepo := memory.NewBookingRepository("partial")
	branchRepo := memory.NewBranchRepository()
	bookingHandler := NewBookingHandler(
		service.NewBookingService(bookingRepo, branchRepo),
		newTestAdminActionService(),
	)
	authService := service.NewAuthService(memory.NewAdminRepository(), memory.NewSessionRepository())

	r := chi.NewRouter()
	r.With(middleware.RequireAuth(authService)).Get("/api/bookings", bookingHandler.GetBookings)
	r.With(middleware.RequireAuth(authService)).Get("/api/bookings", bookingHandler.GetBookings)
	r.With(middleware.RequireAuth(authService)).Delete("/api/bookings/{id}", bookingHandler.CancelBooking)

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{name: "list bookings", method: http.MethodGet, path: "/api/bookings"},
		{name: "lookup bookings by email", method: http.MethodGet, path: "/api/bookings?email=harry@example.com"},
		{name: "cancel booking", method: http.MethodDelete, path: "/api/bookings/123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("Expected status %d, got %d: %s", http.StatusUnauthorized, w.Code, w.Body.String())
			}
		})
	}
}

func TestBookingHandler_CheckAvailability_Success(t *testing.T) {
	bookingRepo := memory.NewBookingRepository("partial")
	branchRepo := memory.NewBranchRepository()
	svc := service.NewBookingService(bookingRepo, branchRepo)
	handler := NewBookingHandler(svc, newTestAdminActionService())

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
	handler := NewBookingHandler(svc, newTestAdminActionService())

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
	handler := NewBookingHandler(svc, newTestAdminActionService())

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
	handler := NewBookingHandler(svc, newTestAdminActionService())

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
	handler := NewBookingHandler(svc, newTestAdminActionService())

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
	handler := NewBookingHandler(svc, newTestAdminActionService())

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
	handler := NewBookingHandler(svc, newTestAdminActionService())

	r := chi.NewRouter()
	r.Get("/bookings", handler.GetBookings)

	req := httptest.NewRequest("GET", "/bookings?email=customer1@example.com&limit=1&offset=0", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

func TestBookingHandler_GetBookingsByEmail_Pagination_InvalidLimit(t *testing.T) {
	bookingRepo := memory.NewBookingRepository("partial")
	branchRepo := memory.NewBranchRepository()
	svc := service.NewBookingService(bookingRepo, branchRepo)
	handler := NewBookingHandler(svc, newTestAdminActionService())

	r := chi.NewRouter()
	r.Get("/bookings", handler.GetBookings)

	req := httptest.NewRequest("GET", "/bookings?email=harry@hogwarts.edu&limit=2000", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestBookingHandler_CreateBooking_CancelledIdempotentBookingReturnsConflict(t *testing.T) {
	bookingRepo := memory.NewBookingRepository("empty")
	handler := NewBookingHandler(service.NewBookingService(bookingRepo, memory.NewBranchRepository()), nil)
	booking := domain.Booking{
		BranchID:     "550e8400-e29b-41d4-a716-446655440001",
		ServiceID:    "660e8400-e29b-41d4-a716-446655440001",
		CustomerName: "Ron Weasley",
		Email:        "ron@example.com",
		Phone:        "+27 82 123 4567",
		Date:         time.Now().AddDate(0, 0, 30).Format("2006-01-02"),
		TimeSlot:     "12:00",
	}
	body, err := json.Marshal(booking)
	if err != nil {
		t.Fatal(err)
	}
	request := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/bookings", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "cancelled-request-456")
		response := httptest.NewRecorder()
		handler.CreateBooking(response, req)
		return response
	}

	first := request()
	if first.Code != http.StatusCreated {
		t.Fatalf("first status = %d, want 201: %s", first.Code, first.Body)
	}
	var created domain.Booking
	if err := json.Unmarshal(first.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if err := bookingRepo.Cancel(context.Background(), created.ID); err != nil {
		t.Fatalf("cancel booking: %v", err)
	}

	retry := request()
	assertJSONError(t, retry, http.StatusConflict, "booking_cancelled")
}

func TestBookingHandler_CreateBooking_ReplaysAcrossPhoneFormats(t *testing.T) {
	bookingRepo := memory.NewBookingRepository("empty")
	handler := NewBookingHandler(service.NewBookingService(bookingRepo, memory.NewBranchRepository()), nil)
	booking := domain.Booking{
		BranchID:     "550e8400-e29b-41d4-a716-446655440001",
		ServiceID:    "660e8400-e29b-41d4-a716-446655440001",
		CustomerName: "Ron Weasley",
		Email:        "ron@example.com",
		Phone:        "+27 82 123 4567",
		Date:         time.Now().AddDate(0, 0, 30).Format("2006-01-02"),
		TimeSlot:     "13:00",
	}
	request := func(phone string) *httptest.ResponseRecorder {
		booking.Phone = phone
		body, err := json.Marshal(booking)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/bookings", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "phone-format-789")
		response := httptest.NewRecorder()
		handler.CreateBooking(response, req)
		return response
	}

	first := request("+27 82 123 4567")
	second := request("082 123 4567")
	if first.Code != http.StatusCreated || second.Code != http.StatusCreated {
		t.Fatalf("statuses = %d and %d, want 201: %s / %s", first.Code, second.Code, first.Body, second.Body)
	}
}
