// Package handler contains HTTP request handlers that form the presentation layer.
// Handlers are responsible for:
// - Parsing and validating HTTP requests
// - Calling appropriate service methods
// - Formatting and returning HTTP responses
// - Error handling and logging
package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/GringottsBankBranchBooking/internal/domain"
	"github.com/GringottsBankBranchBooking/internal/service"
	"github.com/GringottsBankBranchBooking/pkg/clock"
	"github.com/GringottsBankBranchBooking/pkg/validator"
	"github.com/go-chi/chi/v5"
	"log/slog"
)

// BookingHandler handles HTTP requests related to booking operations.
// It provides RESTful endpoints for creating bookings, retrieving booking information,
// and checking availability. All input validation and error handling is performed here.
type BookingHandler struct {
	bookingService *service.BookingService // Service layer for booking business logic
	auditService   *service.AuditService   // Records which admin performed privileged actions
	clock          clock.Clock             // Source of "now" for date and time slot validation
}

// BookingHandlerOption customises a BookingHandler at construction time.
type BookingHandlerOption func(*BookingHandler)

// WithClock overrides the clock used to decide whether a requested date or time
// slot is in the past. The default reports time in the business time zone.
func WithClock(c clock.Clock) BookingHandlerOption {
	return func(h *BookingHandler) {
		h.clock = c
	}
}

// NewBookingHandler creates a new BookingHandler with the required service dependencies.
//
// Parameters:
//   - bookingService: Service instance for handling booking business logic
//   - auditService: Service instance for recording admin actions
//   - opts: Optional settings, such as WithClock
//
// Returns:
//   - *BookingHandler: Configured handler ready to process HTTP requests
func NewBookingHandler(bookingService *service.BookingService, auditService *service.AuditService, opts ...BookingHandlerOption) *BookingHandler {
	h := &BookingHandler{
		bookingService: bookingService,
		auditService:   auditService,
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
			writeError(w, http.StatusBadRequest, "Invalid limit: must be between 1 and 1000")
			return 0, 0, false
		}
	}

	// Parse offset from query string
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		var err error
		offset, err = strconv.Atoi(offsetStr)
		if err != nil || offset < 0 {
			writeError(w, http.StatusBadRequest, "Invalid offset: must be a non-negative integer")
			return 0, 0, false
		}
	}

	return limit, offset, true
}

// CreateBooking handles POST /api/bookings requests to create new bookings.
// It validates all input data, checks business rules via the service layer,
// and returns appropriate HTTP responses with proper error handling.
//
// Request Body: JSON representation of domain.Booking
// Response: 200 with created booking, 400 for validation errors, 409 for conflicts, 500 for server errors
//
// Security: All inputs are validated and sanitized before processing
func (h *BookingHandler) CreateBooking(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Parse JSON request body into booking struct
	var booking domain.Booking
	if err := json.NewDecoder(r.Body).Decode(&booking); err != nil {
		slog.Error("Operation failed", "operation", "decode booking request", "error", err.Error())
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate all input fields to prevent injection attacks and ensure data integrity
	if !validator.ValidateEmail(booking.Email) {
		writeError(w, http.StatusBadRequest, "Invalid email format")
		return
	}
	if !validator.ValidatePhone(booking.Phone) {
		writeError(w, http.StatusBadRequest, "Invalid phone format")
		return
	}
	if !validator.ValidateCustomerName(booking.CustomerName) {
		writeError(w, http.StatusBadRequest, "Invalid customer name format")
		return
	}
	if !validator.ValidateServiceID(booking.ServiceID) {
		writeError(w, http.StatusBadRequest, "Invalid service ID format")
		return
	}
	// Read the clock once so the date and slot checks agree on what "now" is
	now := h.clock.Now()
	if !validator.ValidateDate(booking.Date, now) {
		writeError(w, http.StatusBadRequest, "Invalid date format or date in the past")
		return
	}
	if !validator.ValidateTimeSlot(booking.TimeSlot) {
		writeError(w, http.StatusBadRequest, "Invalid time slot format")
		return
	}
	// A slot earlier today passes the date check above but has already begun
	if !validator.ValidateSlotNotPast(booking.Date, booking.TimeSlot, now) {
		writeError(w, http.StatusBadRequest, "Time slot is in the past")
		return
	}
	if !validator.ValidateBranchID(booking.BranchID) {
		writeError(w, http.StatusBadRequest, "Invalid branch ID")
		return
	}

	// Attempt to create booking through service layer
	if err := h.bookingService.CreateBooking(r.Context(), &booking); err != nil {
		// Handle specific business logic errors with appropriate HTTP status codes
		if err == domain.ErrCapacityExceeded {
			slog.Info("Booking capacity exceeded", "branch_id", booking.BranchID, "date", booking.Date, "time_slot", booking.TimeSlot)
			writeError(w, http.StatusConflict, "Branch capacity exceeded for this time slot")
			return
		}
		// Log internal errors but don't expose details to prevent information leakage
		slog.Error("Operation failed", "operation", "create booking", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "Failed to create booking")
		return
	}

	// Success: log the operation and return the created booking
	slog.Info("Booking created successfully", "branch_id", booking.BranchID)
	if err := json.NewEncoder(w).Encode(booking); err != nil {
		slog.Error("Operation failed", "operation", "encode booking response", "error", err.Error())
	}
}

// GetBookings handles GET /api/bookings requests to retrieve all bookings.
// Supports pagination via query parameters: ?limit=50&offset=0
// Default limit is 100 if not provided. Limit must be 1-1000.
//
// Response: 200 with array of bookings, 400 for invalid params, 500 for server errors
func (h *BookingHandler) GetBookings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Parse and validate pagination parameters
	limit, offset, ok := parsePaginationParams(w, r)
	if !ok {
		return
	}

	// Retrieve bookings from service layer with pagination
	bookings, err := h.bookingService.GetAllBookingsPaginated(r.Context(), limit, offset)
	if err != nil {
		slog.Error("Operation failed", "operation", "get all bookings", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "Failed to get bookings")
		return
	}

	// Return bookings as JSON response
	if err := json.NewEncoder(w).Encode(bookings); err != nil {
		slog.Error("Operation failed", "operation", "encode bookings response", "error", err.Error())
	}
}

// GetBookingsByEmail handles GET /api/bookings/{email} requests to retrieve bookings for a specific customer.
// Supports pagination via query parameters: ?limit=50&offset=0
// Default limit is 100 if not provided. Limit must be 1-1000.
//
// URL Parameters: email - customer's email address
// Response: 200 with array of bookings, 400 for invalid params, 500 for server errors
func (h *BookingHandler) GetBookingsByEmail(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Extract and validate email parameter from URL
	email := chi.URLParam(r, "email")

	// Validate email parameter to prevent injection attacks
	if !validator.ValidateEmail(email) {
		writeError(w, http.StatusBadRequest, "Invalid email format")
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
		slog.Error("Operation failed", "operation", "get bookings by email", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "Failed to get bookings")
		return
	}

	// Return customer's bookings as JSON response
	if err := json.NewEncoder(w).Encode(bookings); err != nil {
		slog.Error("Operation failed", "operation", "encode bookings by email response", "error", err.Error())
	}
}

// CheckAvailability handles GET /availability/{branchId}/{date} requests to check time slot availability.
// Returns a map indicating which time slots are available for booking at the specified branch and date.
//
// URL Parameters:
//   - branchId: ID of the branch to check
//   - date: Date to check in YYYY-MM-DD format
//
// Response: 200 with availability map, 400 for invalid parameters, 500 for server errors
func (h *BookingHandler) CheckAvailability(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Extract URL parameters
	branchID := chi.URLParam(r, "branchId")
	date := chi.URLParam(r, "date")

	// Validate parameters to ensure data integrity and prevent injection attacks
	if !validator.ValidateBranchID(branchID) {
		writeError(w, http.StatusBadRequest, "Invalid branch ID")
		return
	}
	if !validator.ValidateDate(date, h.clock.Now()) {
		writeError(w, http.StatusBadRequest, "Invalid date format")
		return
	}

	// Check availability through service layer
	bookedSlots, err := h.bookingService.CheckAvailability(r.Context(), branchID, date)
	if err != nil {
		slog.Error("Operation failed", "operation", "check availability", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "Failed to check availability")
		return
	}

	// Return availability information as JSON (true = booked, false = available)
	if err := json.NewEncoder(w).Encode(bookedSlots); err != nil {
		slog.Error("Operation failed", "operation", "encode availability response", "error", err.Error())
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
	if bookingID == "" {
		writeError(w, http.StatusBadRequest, "Invalid booking ID")
		return
	}

	// Cancel booking through service layer
	if err := h.bookingService.CancelBooking(r.Context(), bookingID); err != nil {
		if err == domain.ErrBookingNotFound {
			writeError(w, http.StatusNotFound, "Booking not found")
			return
		}
		slog.Error("Operation failed", "operation", "cancel booking", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "Failed to cancel booking")
		return
	}

	// Record who cancelled which booking now that the cancellation has succeeded
	h.auditService.Record(r.Context(), adminID, domain.AuditActionBookingCancel, domain.AuditTargetBooking, bookingID, "")

	// Success response
	slog.Info("Booking cancelled successfully", "booking_id", bookingID)
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "cancelled"}); err != nil {
		slog.Error("Operation failed", "operation", "encode cancel response", "error", err.Error())
	}
}
