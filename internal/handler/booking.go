// Package handler contains HTTP request handlers that form the presentation layer.
// Handlers are responsible for:
// - Parsing and validating HTTP requests
// - Calling appropriate service methods
// - Formatting and returning HTTP responses
// - Error handling and logging
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"log/slog"

	"github.com/go-chi/chi/v5"
	"github.com/gringottsbankbranchbooking/internal/apierror"
	"github.com/gringottsbankbranchbooking/internal/domain"
	"github.com/gringottsbankbranchbooking/internal/middleware"
	"github.com/gringottsbankbranchbooking/internal/service"
	"github.com/gringottsbankbranchbooking/pkg/clock"
	"github.com/gringottsbankbranchbooking/pkg/validator"
)

const maxBookingBodyBytes = 16 * 1024
const maxIdempotencyKeyBytes = 128

// BookingHandler handles HTTP requests related to booking operations.
// It provides RESTful endpoints for creating bookings, retrieving booking information,
// and checking availability. It validates URL and query parameters, calls services,
// and formats HTTP responses and errors.
type BookingHandler struct {
	bookingService *service.BookingService // Service layer for booking business logic
	adminActions   *service.AdminActionService
	clock          clock.Clock // Source of "now" for date and time slot validation
	requestLimiter bookingRequestLimiter
}

func validIdempotencyKey(key string) bool {
	if key == "" {
		return true
	}
	if len(key) > maxIdempotencyKeyBytes {
		return false
	}
	for index := 0; index < len(key); index++ {
		if key[index] < 0x21 || key[index] > 0x7e {
			return false
		}
	}
	return true
}

// BookingHandlerOption customises a BookingHandler at construction time.
type BookingHandlerOption func(*BookingHandler)

// WithClock overrides the clock used to decide whether a requested date or time
// slot is in the past. The default reports time in the business time zone.
func WithClock(c clock.Clock) BookingHandlerOption {
	return func(h *BookingHandler) {
		h.clock = c
		if h.bookingService != nil {
			h.bookingService.SetClock(c)
		}
	}
}

// NewBookingHandler creates a new BookingHandler with the required service dependencies.
//
// Parameters:
//   - bookingService: Service instance for handling booking business logic
//   - adminActions: Service that atomically performs and audits privileged actions
//   - opts: Optional settings, such as WithClock
//
// Returns:
//   - *BookingHandler: Configured handler ready to process HTTP requests
func NewBookingHandler(bookingService *service.BookingService, adminActions *service.AdminActionService, opts ...BookingHandlerOption) *BookingHandler {
	h := &BookingHandler{
		bookingService: bookingService,
		adminActions:   adminActions,
		clock:          clock.NewBusinessClock(),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// parsePaginationParams extracts and validates pagination parameters from query string.
// Default limit is 100, default offset is 0. Returns error responses if params are invalid.
func parsePaginationParams(w http.ResponseWriter, r *http.Request) (limit, offset int, ok bool) {
	limit = 100 // default limit
	offset = 0  // default offset

	// Parse limit from query string
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		var err error
		limit, err = strconv.Atoi(limitStr)
		if err != nil || limit < 1 || limit > 1000 {
			apierror.Write(w, http.StatusBadRequest, "Invalid limit: must be between 1 and 1000")
			return 0, 0, false
		}
	}

	// Parse offset from query string
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		var err error
		offset, err = strconv.Atoi(offsetStr)
		if err != nil || offset < 0 {
			apierror.Write(w, http.StatusBadRequest, "Invalid offset: must be a non-negative integer")
			return 0, 0, false
		}
	}

	return limit, offset, true
}

// CreateBooking handles POST /api/bookings requests to create new bookings.
// It validates all input data, checks business rules via the service layer,
// and returns appropriate HTTP responses with proper error handling.
//
// Request Body: JSON representation of domain.Booking; server-assigned fields
// (id, status, created_at) are ignored
// Header: optional Idempotency-Key (1-128 visible ASCII); a retry with the same key and
// the same request replays the original 201 instead of creating a second booking.
// Response: 201 with created booking, 400 for validation errors or a malformed
// Idempotency-Key, 404 for a missing branch, 409 for a full slot, a key reused with a
// different request, or a key whose booking was cancelled, 413 for bodies exceeding
// 16 KiB, 415 for a non-JSON Content-Type, 429 with Retry-After for rate or
// tracking-capacity limits, 503 with Retry-After when the database is unavailable
// (safe to retry with the same key), and 500 for other server errors.
//
// Security: All inputs are validated and sanitized before processing
func (h *BookingHandler) CreateBooking(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if retryAfter := h.requestLimiter.allow(clientIPFromRemoteAddr(r.RemoteAddr), time.Now()); retryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
		apierror.Write(w, http.StatusTooManyRequests, "Too many booking attempts. Please try again later.")
		return
	}
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if !validIdempotencyKey(idempotencyKey) {
		apierror.Write(w, http.StatusBadRequest, "Idempotency-Key must contain 1 to 128 visible ASCII characters")
		return
	}
	if !requireJSONContentType(w, r) {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBookingBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var sizeError *http.MaxBytesError
		if errors.As(err, &sizeError) {
			apierror.Write(w, http.StatusRequestEntityTooLarge, "Booking request body exceeds 16 KiB")
			return
		}
		apierror.Write(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Parse JSON request body into booking struct
	var booking domain.Booking
	if err := json.Unmarshal(body, &booking); err != nil {
		slog.ErrorContext(r.Context(), "Operation failed", "operation", "decode booking request", "error", err.Error())
		apierror.Write(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// id, status and created_at are server-assigned; discard anything the client sent.
	booking.ID = ""

	// Attempt to create booking through service layer
	createBooking := h.bookingService.CreateBooking
	if idempotencyKey != "" {
		createBooking = func(ctx context.Context, booking *domain.Booking) error {
			return h.bookingService.CreateBookingIdempotent(ctx, booking, idempotencyKey)
		}
	}
	if err := createBooking(r.Context(), &booking); err != nil {
		// Handle specific business logic errors with appropriate HTTP status codes.
		// Expected missing resource cases should not be treated as internal failures.
		var validationErr *service.BookingValidationError
		if errors.As(err, &validationErr) {
			if errors.Is(err, domain.ErrBookingSlotInPast) {
				apierror.WriteWithCode(w, http.StatusBadRequest, "slot_in_past", validationErr.Error())
				return
			}
			apierror.Write(w, http.StatusBadRequest, validationErr.Error())
			return
		}
		if errors.Is(err, domain.ErrBranchNotFound) {
			apierror.WriteWithCode(w, http.StatusNotFound, "branch_not_found", "Branch not found")
			return
		}
		if errors.Is(err, domain.ErrServiceNotAvailableAtBranch) {
			apierror.WriteWithCode(w, http.StatusBadRequest, "service_not_offered", "Service is not offered at this branch")
			return
		}
		if errors.Is(err, domain.ErrCapacityExceeded) {
			slog.InfoContext(r.Context(), "Booking capacity exceeded", "branch_id", booking.BranchID, "date", booking.Date, "time_slot", booking.TimeSlot)
			apierror.WriteWithCode(w, http.StatusConflict, "slot_full", "Branch capacity exceeded for this time slot")
			return
		}
		if errors.Is(err, domain.ErrIdempotencyKeyReused) {
			middleware.SetOutcome(r.Context(), "idempotency_key_reused")
			apierror.WriteWithCode(w, http.StatusConflict, "idempotency_key_reused", "Idempotency-Key was already used for a different booking request")
			return
		}
		if errors.Is(err, domain.ErrIdempotentBookingCancelled) {
			middleware.SetOutcome(r.Context(), "booking_cancelled")
			apierror.WriteWithCode(w, http.StatusConflict, "booking_cancelled", "The booking for this Idempotency-Key has been cancelled. Create a new booking with a new Idempotency-Key.")
			return
		}
		// Log internal errors but don't expose details to prevent information leakage
		slog.ErrorContext(r.Context(), "Operation failed", "operation", "create booking", "error", err.Error())
		apierror.WriteFailure(w, err, "Failed to create booking")
		return
	}

	// Success: log the operation and return the created booking
	slog.InfoContext(r.Context(), "Booking created successfully", "booking_id", booking.ID, "branch_id", booking.BranchID, "date", booking.Date, "time_slot", booking.TimeSlot)
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(booking); err != nil {
		slog.ErrorContext(r.Context(), "Operation failed", "operation", "encode booking response", "error", err.Error())
	}
}

// GetBookings handles GET /api/bookings requests to retrieve all bookings.
// Supports pagination via query parameters: ?limit=50&offset=0
// Default limit is 100 if not provided. Limit must be 1-1000.
//
// Response: 200 with array of bookings, 400 for invalid params, 401 if not authenticated
// (enforced by RequireAuth middleware on this route), 503 when the
// database is unavailable, 500 for other server errors
func (h *BookingHandler) GetBookings(w http.ResponseWriter, r *http.Request) {
	if _, ok := r.URL.Query()["email"]; ok {
		h.GetBookingsByEmail(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")

	// Parse and validate pagination parameters
	limit, offset, ok := parsePaginationParams(w, r)
	if !ok {
		return
	}

	// Retrieve bookings from service layer with pagination
	bookings, err := h.bookingService.GetAllBookingsPaginated(r.Context(), limit, offset)
	if err != nil {
		slog.ErrorContext(r.Context(), "Operation failed", "operation", "get all bookings", "error", err.Error())
		apierror.WriteFailure(w, err, "Failed to get bookings")
		return
	}

	// Return bookings as JSON response
	if err := json.NewEncoder(w).Encode(bookings); err != nil {
		slog.ErrorContext(r.Context(), "Operation failed", "operation", "encode bookings response", "error", err.Error())
	}
}

// GetBookingsByEmail handles GET /api/bookings?email={email} requests to retrieve bookings for a specific customer.
// Supports pagination via query parameters: ?limit=50&offset=0
// Default limit is 100 if not provided. Limit must be 1-1000.
//
// Query Parameters: email - customer's email address
// Response: 200 with array of bookings, 400 for invalid params, 500 for server errors
func (h *BookingHandler) GetBookingsByEmail(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store") // customer PII must not be cached

	// Extract and validate email parameter from the query string
	email := r.URL.Query().Get("email")

	// Validate email parameter to prevent injection attacks
	if !validator.ValidateEmail(email) {
		apierror.Write(w, http.StatusBadRequest, "Invalid email format")
		return
	}

	// Parse and validate pagination parameters
	limit, offset, ok := parsePaginationParams(w, r)
	if !ok {
		return
	}

	// Retrieve bookings for the specified email with pagination
	bookings, err := h.bookingService.GetBookingsByEmailPaginated(r.Context(), email, limit, offset)
	if err != nil {
		slog.ErrorContext(r.Context(), "Operation failed", "operation", "get bookings by email", "error", err.Error())
		apierror.WriteFailure(w, err, "Failed to get bookings")
		return
	}

	// Return customer's bookings as JSON response
	if err := json.NewEncoder(w).Encode(bookings); err != nil {
		slog.ErrorContext(r.Context(), "Operation failed", "operation", "encode bookings by email response", "error", err.Error())
	}
}

// CheckAvailability handles GET /api/availability/{branchId}/{date} requests to check time slot availability.
// Returns a map indicating which time slots are available for booking at the specified branch and date.
//
// URL Parameters:
//   - branchId: ID of the branch to check
//   - date: Date to check in YYYY-MM-DD format
//
// Response: 200 with availability map, 400 for invalid parameters, 404 for an unknown branch, 500 for server errors
func (h *BookingHandler) CheckAvailability(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Extract URL parameters
	branchID := chi.URLParam(r, "branchId")
	date := chi.URLParam(r, "date")

	// Validate parameters to ensure data integrity and prevent injection attacks
	if !validator.ValidateBranchID(branchID) {
		apierror.Write(w, http.StatusBadRequest, "Invalid branch ID")
		return
	}
	now := h.clock.Now()
	if !validator.ValidateDate(date, now) {
		apierror.Write(w, http.StatusBadRequest, "Invalid date format or date before today")
		return
	}

	// Check availability through service layer
	bookedSlots, err := h.bookingService.CheckAvailability(r.Context(), branchID, date)
	if err != nil {
		if errors.Is(err, domain.ErrBranchNotFound) {
			apierror.Write(w, http.StatusNotFound, "Branch not found")
			return
		}
		slog.ErrorContext(r.Context(), "Operation failed", "operation", "check availability", "error", err.Error())
		apierror.WriteFailure(w, err, "Failed to check availability")
		return
	}
	// Slots that have started today cannot be booked, even if capacity remains.
	if date == now.Format("2006-01-02") {
		for _, slot := range validator.BookableTimeSlots() {
			if !validator.ValidateSlotNotPast(date, slot, now) {
				bookedSlots[slot] = true
			}
		}
	}

	// Return availability information as JSON (true = at capacity, false = available)
	if err := json.NewEncoder(w).Encode(bookedSlots); err != nil {
		slog.ErrorContext(r.Context(), "Operation failed", "operation", "encode availability response", "error", err.Error())
	}
}

// CancelBooking handles DELETE /api/bookings/{id} requests to cancel existing bookings.
// This endpoint is used by admin users to cancel bookings on behalf of customers.
//
// URL Parameters: id - booking ID to cancel
// Response: 200 for success, 400 for invalid ID, 401 if not authenticated,
// 404 for not found, 500 for server errors
func (h *BookingHandler) CancelBooking(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Cancelling is a privileged action, so it must be attributable to an admin
	adminID, ok := requireAdminID(w, r)
	if !ok {
		return
	}

	// Extract booking ID from URL
	bookingID := chi.URLParam(r, "id")

	// Validate booking ID format
	if !validator.ValidateIdentifier(bookingID) {
		apierror.Write(w, http.StatusBadRequest, "Invalid booking ID")
		return
	}

	// Cancel booking through service layer
	if err := h.adminActions.CancelBooking(r.Context(), bookingID, adminID); err != nil {
		if errors.Is(err, domain.ErrBookingNotFound) {
			apierror.Write(w, http.StatusNotFound, "Booking not found")
			return
		}
		slog.ErrorContext(r.Context(), "Operation failed", "operation", "cancel booking", "error", err.Error())
		apierror.WriteFailure(w, err, "Failed to cancel booking")
		return
	}

	// Success response
	slog.InfoContext(r.Context(), "Booking cancelled successfully", "booking_id", bookingID)
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "cancelled"}); err != nil {
		slog.ErrorContext(r.Context(), "Operation failed", "operation", "encode cancel response", "error", err.Error())
	}
}
