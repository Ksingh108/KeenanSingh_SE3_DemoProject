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
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/GringottsBankBranchBooking/internal/domain"
	"github.com/GringottsBankBranchBooking/internal/service"
	"github.com/GringottsBankBranchBooking/pkg/validator"
	"log/slog"
)

// BranchHandler handles HTTP requests related to branch operations.
// It provides endpoints to list, create, and delete branches. Input validation,
// error handling and response encoding are performed here. The handler delegates
// business logic to the service layer (`service.BranchService`).
type BranchHandler struct {
	branchService *service.BranchService
	auditService  *service.AuditService
}

// NewBranchHandler constructs a new BranchHandler with the provided services.
// Parameters:
//   - branchService: service instance responsible for branch business logic
//   - auditService: service instance that records admin actions
//
// Returns:
//   - *BranchHandler: ready-to-use HTTP handler for branch endpoints
func NewBranchHandler(branchService *service.BranchService, auditService *service.AuditService) *BranchHandler {
	return &BranchHandler{
		branchService: branchService,
		auditService:  auditService,
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
		slog.Error("Operation failed", "operation", "get all branches", "error", err.Error())
		http.Error(w, "Failed to get branches", http.StatusInternalServerError)
		return
	}

	// Write JSON response. Set content type and stream the encoded branches.
	// If encoding fails, log the error — there's little we can do at this point
	// other than report a server error in logs.
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(branches); err != nil {
		slog.Error("Operation failed", "operation", "encode branches response", "error", err.Error())
	}
}

// CreateBranch handles POST /admin/branches requests to create a new branch.
// It decodes and validates the request body, ensures required fields are present,
// and delegates creation to the branch service. Responses:
//   - 201: created branch object
//   - 400: invalid request or fields that fail validation
//   - 401: request is not authenticated as an admin
//   - 500: server error during creation
func (h *BranchHandler) CreateBranch(w http.ResponseWriter, r *http.Request) {
	// Creating a branch is a privileged action, so it must be attributable to an admin
	adminID, ok := requireAdminID(w, r)
	if !ok {
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Bad input — inform the client without exposing internal details.
		slog.Error("Operation failed", "operation", "failed to decode create branch json", "error", err.Error())
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Validate every field with the allow-list validators so that branch input
	// gets the same defense-in-depth as the public booking path. Messages name
	// the field but never echo the rejected value back.
	if !validator.ValidateBranchName(req.Name) {
		http.Error(w, "Invalid branch name", http.StatusBadRequest)
		return
	}
	if !validator.ValidateCity(req.City) {
		http.Error(w, "Invalid city", http.StatusBadRequest)
		return
	}
	if !validator.ValidateAddress(req.Address) {
		http.Error(w, "Invalid address", http.StatusBadRequest)
		return
	}
	if !validator.ValidatePhone(req.Phone) {
		http.Error(w, "Invalid phone number", http.StatusBadRequest)
		return
	}
	if !validator.ValidateCapacity(req.Capacity) {
		http.Error(w, "Capacity must be between 1 and 1000", http.StatusBadRequest)
		return
	}
	if len(req.Services) == 0 {
		http.Error(w, "At least one service must be selected", http.StatusBadRequest)
		return
	}

	// Each service ID must be well-formed and appear only once; a duplicate
	// would otherwise surface as a primary-key violation and a 500.
	seenServices := make(map[string]struct{}, len(req.Services))
	for _, serviceID := range req.Services {
		if !validator.ValidateServiceID(serviceID) {
			http.Error(w, "Invalid service ID", http.StatusBadRequest)
			return
		}
		if _, duplicate := seenServices[serviceID]; duplicate {
			http.Error(w, "Duplicate service ID", http.StatusBadRequest)
			return
		}
		seenServices[serviceID] = struct{}{}
	}

	// Delegate branch creation to the service layer which handles persistence
	// and any related business rules. The handler passes already-validated data.
	branch, err := h.branchService.CreateBranch(r.Context(), req.Name, req.City, req.Address, req.Phone, req.Capacity, req.Services)
	if err != nil {
		// Log the error for diagnostics and return a generic error to clients.
		slog.Error("Operation failed", "operation", "create branch", "error", err.Error())
		http.Error(w, "Failed to create branch", http.StatusInternalServerError)
		return
	}

	// Record who created the branch now that it has been persisted
	h.auditService.Record(r.Context(), adminID, domain.AuditActionBranchCreate, domain.AuditTargetBranch, branch.ID, "name="+branch.Name)

	// Respond with the created branch as JSON.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(branch); err != nil {
		slog.Error("Operation failed", "operation", "encode branch response", "error", err.Error())
	}
}

// DeleteBranch handles DELETE /admin/branches/{id} requests to remove a branch.
// It reads the branch ID from the route parameter, validates it, and delegates
// deletion to the branch service. Responses:
//   - 204: successfully deleted
//   - 400: missing or malformed branch ID
//   - 401: request is not authenticated as an admin
//   - 404: branch does not exist
//   - 500: server error during deletion
func (h *BranchHandler) DeleteBranch(w http.ResponseWriter, r *http.Request) {
	// Deleting a branch is a privileged action, so it must be attributable to an admin
	adminID, ok := requireAdminID(w, r)
	if !ok {
		return
	}

	id := chi.URLParam(r, "id")
	if !validator.ValidateBranchID(id) {
		writeError(w, http.StatusBadRequest, "Invalid branch ID")
		return
	}

	// Delegate deletion to the service layer. The repository reports
	// domain.ErrBranchNotFound when no branch matched the ID.
	bookingsRemoved, err := h.branchService.DeleteBranch(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrBranchNotFound) {
			writeError(w, http.StatusNotFound, "Branch not found")
			return
		}
		slog.Error("Operation failed", "operation", "delete branch", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "Failed to delete branch")
		return
	}

	// Record who deleted the branch and how many bookings went with it, since
	// the database cascades the delete to them without any other trace.
	h.auditService.Record(r.Context(), adminID, domain.AuditActionBranchDelete, domain.AuditTargetBranch, id, fmt.Sprintf("bookings_removed=%d", bookingsRemoved))

	// Successful deletion: respond with 204 No Content.
	w.WriteHeader(http.StatusNoContent)
}
