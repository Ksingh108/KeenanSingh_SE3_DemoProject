// Package handler provides HTTP handlers for the Gringotts Bank Branch Booking System.
// This package implements the presentation layer of Clean Architecture, handling HTTP requests,
// input validation, and response formatting while delegating business logic to service layer.
package handler

import (
	"encoding/json"
	"net/http"

	"log/slog"

	"github.com/gringottsbankbranchbooking/internal/apierror"
	"github.com/gringottsbankbranchbooking/internal/service"
)

// BranchServiceHandler handles HTTP requests for banking service operations.
// It provides endpoints for retrieving available banking services that can be offered at branches.
// This handler focuses on service catalog management rather than branch-specific operations.
//
// Security Considerations:
// - All responses use secure logging to prevent information leakage
// - Generic error messages prevent internal system exposure
type BranchServiceHandler struct {
	serviceService *service.ServiceService // Service layer for banking service business logic
}

// NewBranchServiceHandler creates a new BranchServiceHandler with required service dependency.
// Uses dependency injection pattern for testability and loose coupling.
//
// Parameters:
//   - serviceService: Service for banking service operations and management
//
// Returns:
//   - *BranchServiceHandler: Configured handler ready for HTTP routing
func NewBranchServiceHandler(serviceService *service.ServiceService) *BranchServiceHandler {
	return &BranchServiceHandler{
		serviceService: serviceService,
	}
}

// GetServices handles GET /api/services requests to retrieve all available banking services.
// This endpoint is used by admin forms to populate service selection options when creating branches.
// Returns a list of all banking services that can be offered at branch locations.
//
// Request: GET /api/services (no parameters required)
//
// Response:
//   - 200 OK: JSON array of banking services with ID, name, and description
//   - 500 Internal Server Error: Generic error message for server failures
//
// Security Features:
// - Secure logging prevents log injection vulnerabilities
// - Generic error messages prevent internal system exposure
// - No input validation required as endpoint accepts no parameters
func (h *BranchServiceHandler) GetServices(w http.ResponseWriter, r *http.Request) {
	// Retrieve all available banking services from service layer
	services, err := h.serviceService.GetAllServices(r.Context())
	if err != nil {
		// Log internal errors but don't expose details to prevent information leakage
		slog.ErrorContext(r.Context(), "Operation failed", "operation", "get all services", "error", err.Error())
		apierror.WriteFailure(w, err, "Failed to get services")
		return
	}

	// Return services as JSON response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(services); err != nil {
		slog.ErrorContext(r.Context(), "Operation failed", "operation", "encode services response", "error", err.Error())
	}
}
