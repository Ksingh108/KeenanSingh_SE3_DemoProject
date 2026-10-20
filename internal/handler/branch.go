// Package handler contains HTTP request handlers that form the presentation layer.
// Handlers are responsible for:
// - Parsing and validating HTTP requests
// - Calling appropriate service methods
// - Formatting and returning HTTP responses
// - Error handling and logging
package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"log/slog"

	"github.com/gringottsbankbranchbooking/internal/apierror"
	"github.com/gringottsbankbranchbooking/internal/domain"
	"github.com/gringottsbankbranchbooking/internal/service"
	"github.com/gringottsbankbranchbooking/pkg/validator"
)

// BranchHandler handles HTTP requests related to branch operations.
// It provides endpoints to list, create, and delete branches. Input validation,
// error handling and response encoding are performed here. The handler delegates
// business logic to the service layer (`service.BranchService`).
type BranchHandler struct {
	branchService *service.BranchService
	adminActions  *service.AdminActionService
}

// NewBranchHandler constructs a new BranchHandler with the provided services.
// Parameters:
//   - branchService: service instance responsible for branch business logic
//   - adminActions: service that atomically performs and audits privileged actions
//
// Returns:
//   - *BranchHandler: ready-to-use HTTP handler for branch endpoints
func NewBranchHandler(branchService *service.BranchService, adminActions *service.AdminActionService) *BranchHandler {
	return &BranchHandler{
		branchService: branchService,
		adminActions:  adminActions,
	}
}

// GetBranches handles GET /api/branches requests to retrieve all branches.
// Response: 200 with JSON array of branches, 500 for server errors.
func (h *BranchHandler) GetBranches(w http.ResponseWriter, r *http.Request) {
	// Request all branches from the service layer. The service is responsible
	// for data retrieval and any business logic; the handler should stay thin.
	branches, err := h.branchService.GetAllBranches(r.Context())
	if err != nil {
		// Log the error for observability but return a generic message to the client
		// to avoid leaking internal details.
		slog.ErrorContext(r.Context(), "Operation failed", "operation", "get all branches", "error", err.Error())
		apierror.WriteFailure(w, err, "Failed to get branches")
		return
	}

	// Write JSON response. Set content type and stream the encoded branches.
	// If encoding fails, log the error — there's little we can do at this point
	// other than report a server error in logs.
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(branches); err != nil {
		slog.ErrorContext(r.Context(), "Operation failed", "operation", "encode branches response", "error", err.Error())
	}
}

const maxBranchBodyBytes = 16 * 1024

// CreateBranch handles POST /admin/branches requests to create a new branch.
// It decodes and validates the request body, ensures required fields are present,
// and delegates creation to the branch service. Responses:
//   - 201: created branch object
//   - 400: invalid request or fields that fail validation
//   - 401: request is not authenticated as an admin
//   - 413: body exceeds 16 KiB
//   - 500: server error during creation
func (h *BranchHandler) CreateBranch(w http.ResponseWriter, r *http.Request) {
	// Creating a branch is a privileged action, so it must be attributable to an admin
	adminID, ok := requireAdminID(w, r)
	if !ok {
		return
	}
	if !requireJSONContentType(w, r) {
		return
	}

	var req struct {
		Name     string   `json:"name"`
		City     string   `json:"city"`
		Address  string   `json:"address"`
		Phone    string   `json:"phone"`
		Capacity int      `json:"capacity"`
		Services []string `json:"services"`
	}

	// Decode the JSON request body into the local request struct. Keeping a
	// small, purpose-built struct here avoids coupling the handler to domain
	// models and makes it easier to validate incoming data.
	r.Body = http.MaxBytesReader(w, r.Body, maxBranchBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var sizeError *http.MaxBytesError
		if errors.As(err, &sizeError) {
			apierror.Write(w, http.StatusRequestEntityTooLarge, "Branch request body exceeds 16 KiB")
			return
		}
		// Bad input — inform the client without exposing internal details.
		slog.ErrorContext(r.Context(), "Operation failed", "operation", "failed to decode create branch json", "error", err.Error())
		apierror.Write(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if err := json.Unmarshal(body, &req); err != nil {
		slog.ErrorContext(r.Context(), "Operation failed", "operation", "failed to decode create branch json", "error", err.Error())
		apierror.Write(w, http.StatusBadRequest, "Invalid request")
		return
	}

	// Validate every field with the allow-list validators so that branch input
	// gets the same defense-in-depth as the public booking path. Messages name
	// the field but never echo the rejected value back.
	if !validator.ValidateBranchName(req.Name) {
		apierror.Write(w, http.StatusBadRequest, "Invalid branch name")
		return
	}
	if !validator.ValidateCity(req.City) {
		apierror.Write(w, http.StatusBadRequest, "Invalid city")
		return
	}
	if !validator.ValidateAddress(req.Address) {
		apierror.Write(w, http.StatusBadRequest, "Invalid address")
		return
	}
	if !validator.ValidatePhone(req.Phone) {
		apierror.Write(w, http.StatusBadRequest, "Invalid phone number: use a South African number, for example +27 82 123 4567 or 082 123 4567")
		return
	}
	req.Phone = validator.NormalizePhone(req.Phone)
	if !validator.ValidateCapacity(req.Capacity) {
		apierror.Write(w, http.StatusBadRequest, "Capacity must be between 1 and 1000")
		return
	}
	if len(req.Services) == 0 {
		apierror.Write(w, http.StatusBadRequest, "At least one service must be selected")
		return
	}

	// Each service ID must be well-formed and appear only once; a duplicate
	// would otherwise surface as a primary-key violation and a 500.
	seenServices := make(map[string]struct{}, len(req.Services))
	for _, serviceID := range req.Services {
		if !validator.ValidateServiceID(serviceID) {
			apierror.Write(w, http.StatusBadRequest, "Invalid service ID")
			return
		}
		if _, duplicate := seenServices[serviceID]; duplicate {
			apierror.Write(w, http.StatusBadRequest, "Duplicate service ID")
			return
		}
		seenServices[serviceID] = struct{}{}
	}

	// Delegate branch creation to the service layer which handles persistence
	// and any related business rules. The handler passes already-validated data.
	branch, err := h.adminActions.CreateBranch(r.Context(), req.Name, req.City, req.Address, req.Phone, req.Capacity, req.Services, adminID)
	if err != nil {
		if errors.Is(err, domain.ErrServiceNotFound) {
			apierror.Write(w, http.StatusBadRequest, "A selected service does not exist")
			return
		}
		// Log the error for diagnostics and return a generic error to clients.
		slog.ErrorContext(r.Context(), "Operation failed", "operation", "create branch", "error", err.Error())
		apierror.WriteFailure(w, err, "Failed to create branch")
		return
	}

	// Respond with the created branch as JSON.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(branch); err != nil {
		slog.ErrorContext(r.Context(), "Operation failed", "operation", "encode branch response", "error", err.Error())
	}
}

// DeleteBranch handles DELETE /admin/branches/{id} requests to archive a branch.
// It reads and validates the branch ID, then delegates archival and audit
// recording to the admin action service. Responses:
//   - 204: branch archived; booking history preserved
//   - 400: missing or malformed branch ID
//   - 401: request is not authenticated as an admin
//   - 404: branch does not exist
//   - 409: active bookings prevent archival
//   - 500: server error during archival
func (h *BranchHandler) DeleteBranch(w http.ResponseWriter, r *http.Request) {
	// Archiving a branch is privileged, so it must be attributable to an admin.
	adminID, ok := requireAdminID(w, r)
	if !ok {
		return
	}

	id := chi.URLParam(r, "id")
	if !validator.ValidateBranchID(id) {
		apierror.Write(w, http.StatusBadRequest, "Invalid branch ID")
		return
	}

	// Delegate archival to the service layer. The repository reports
	// domain.ErrBranchNotFound when no branch matched the ID and
	// domain.ErrBranchDeleteBlockedByBookings when active bookings prevent archival.
	_, err := h.adminActions.DeleteBranch(r.Context(), id, adminID)
	if err != nil {
		if errors.Is(err, domain.ErrBranchNotFound) {
			apierror.Write(w, http.StatusNotFound, "Branch not found")
			return
		}
		if errors.Is(err, domain.ErrBranchDeleteBlockedByBookings) {
			apierror.Write(w, http.StatusConflict, "Branch cannot be archived while active bookings still exist")
			return
		}
		slog.ErrorContext(r.Context(), "Operation failed", "operation", "delete branch", "error", err.Error())
		apierror.WriteFailure(w, err, "Failed to delete branch")
		return
	}

	// Successful archival: respond with 204 No Content.
	w.WriteHeader(http.StatusNoContent)
}
