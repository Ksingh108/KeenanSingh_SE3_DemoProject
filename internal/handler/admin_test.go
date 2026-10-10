package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/GringottsBankBranchBooking/internal/middleware"
	"github.com/GringottsBankBranchBooking/internal/repository/memory"
	"github.com/GringottsBankBranchBooking/internal/service"
)

func TestAdminHandler_Login_InvalidJSON(t *testing.T) {
	adminRepo := memory.NewAdminRepository()
	sessionRepo := memory.NewSessionRepository()
	authService := service.NewAuthService(adminRepo, sessionRepo)
	branchRepo := memory.NewBranchRepository()
	branchService := service.NewBranchService(branchRepo)
	handler := NewAdminHandler(authService, branchService)

	req := httptest.NewRequest("POST", "/admin/login", bytes.NewBufferString("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.Login(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestAdminHandler_Login_InvalidCredentials(t *testing.T) {
	adminRepo := memory.NewAdminRepository()
	sessionRepo := memory.NewSessionRepository()
	authService := service.NewAuthService(adminRepo, sessionRepo)
	branchRepo := memory.NewBranchRepository()
	branchService := service.NewBranchService(branchRepo)
	handler := NewAdminHandler(authService, branchService)

	loginReq := map[string]string{
		"username": "invalid",
		"password": "invalid",
	}
	body, _ := json.Marshal(loginReq)

	req := httptest.NewRequest("POST", "/admin/login", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.Login(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
}

func TestAdminHandler_Logout(t *testing.T) {
	adminRepo := memory.NewAdminRepository()
	sessionRepo := memory.NewSessionRepository()
	authService := service.NewAuthService(adminRepo, sessionRepo)
	branchRepo := memory.NewBranchRepository()
	branchService := service.NewBranchService(branchRepo)
	handler := NewAdminHandler(authService, branchService)

	req := httptest.NewRequest("POST", "/admin/logout", nil)
	w := httptest.NewRecorder()

	handler.Logout(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestAdminHandler_RequireAuth_NoToken(t *testing.T) {
	adminRepo := memory.NewAdminRepository()
	sessionRepo := memory.NewSessionRepository()
	authService := service.NewAuthService(adminRepo, sessionRepo)

	// Test the middleware behavior: missing token returns 401
	req := httptest.NewRequest("GET", "/admin/bookings", nil)
	w := httptest.NewRecorder()

	// Create a simple next handler to verify it's called on success
	called := false
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	// Apply the middleware
	middleware := middleware.RequireAuth(authService)(nextHandler)
	middleware.ServeHTTP(w, req)

	// Verify 401 response without token
	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}

	// Verify next handler was not called
	if called {
		t.Error("Expected next handler not to be called without auth token")
	}
}
