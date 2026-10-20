package handler

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gringottsbankbranchbooking/internal/domain"
	"github.com/gringottsbankbranchbooking/internal/middleware"
	"github.com/gringottsbankbranchbooking/internal/repository/memory"
	"github.com/gringottsbankbranchbooking/internal/service"
)

func TestAdminHandler_Login_InvalidJSON(t *testing.T) {
	adminRepo := memory.NewAdminRepository()
	sessionRepo := memory.NewSessionRepository()
	authService := service.NewAuthService(adminRepo, sessionRepo)
	handler := newTestAdminHandler(t, authService)

	req := httptest.NewRequest("POST", "/admin/login", bytes.NewBufferString("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.Login(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
	assertJSONError(t, w, http.StatusBadRequest)
}

func TestAdminHandler_Login_RequiresJSONContentType(t *testing.T) {
	handler := newTestAdminHandler(t, service.NewAuthService(memory.NewAdminRepository(), memory.NewSessionRepository()))
	req := httptest.NewRequest("POST", "/admin/login", strings.NewReader(`{"username":"admin","password":"admin123"}`))
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()

	handler.Login(w, req)

	assertJSONError(t, w, http.StatusUnsupportedMediaType)
}

func TestAdminHandler_Login_BodyLimit(t *testing.T) {
	adminRepo := memory.NewAdminRepository()
	sessionRepo := memory.NewSessionRepository()
	authService := service.NewAuthService(adminRepo, sessionRepo)
	handler := newTestAdminHandler(t, authService)

	body := `{"username":"admin","password":"admin123"}` + strings.Repeat(" ", maxLoginBodyBytes)
	req := httptest.NewRequest("POST", "/admin/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.Login(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected status %d, got %d", http.StatusRequestEntityTooLarge, w.Code)
	}
	assertJSONError(t, w, http.StatusRequestEntityTooLarge)
}

func TestAdminHandler_Login_RejectsMultipleJSONValues(t *testing.T) {
	handler := newTestAdminHandler(t, service.NewAuthService(memory.NewAdminRepository(), memory.NewSessionRepository()))
	req := httptest.NewRequest("POST", "/admin/login", strings.NewReader(`{"username":"admin","password":"admin123"} {}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.Login(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	assertJSONError(t, w, http.StatusBadRequest)
}

func TestAdminHandler_Login_InvalidCredentials(t *testing.T) {
	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	adminRepo := memory.NewAdminRepository()
	sessionRepo := memory.NewSessionRepository()
	authService := service.NewAuthService(adminRepo, sessionRepo)
	handler := newTestAdminHandler(t, authService)

	loginReq := map[string]string{
		"username": "submitted-user",
		"password": "submitted-password",
	}
	body, _ := json.Marshal(loginReq)

	req := httptest.NewRequest("POST", "/admin/login", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.Login(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
	assertJSONError(t, w, http.StatusUnauthorized)

	logOutput := logs.String()
	if strings.Contains(logOutput, "submitted-user") || strings.Contains(logOutput, "submitted-password") {
		t.Fatalf("login logs contain submitted credentials: %s", logOutput)
	}
	if strings.Contains(logOutput, "Operation failed") || strings.Count(logOutput, "\n") != 1 {
		t.Fatalf("expected one concise login log entry, got: %s", logOutput)
	}
}

func TestAdminHandler_Login_RateLimitUsesClientIP(t *testing.T) {
	adminRepo := memory.NewAdminRepository()
	sessionRepo := memory.NewSessionRepository()
	authService := service.NewAuthService(adminRepo, sessionRepo)
	handler := newTestAdminHandler(t, authService)

	for attempt := 0; attempt < handler.rateLimiter.maxFailedAttempts; attempt++ {
		req := httptest.NewRequest("POST", "/admin/login", bytes.NewBufferString(`{"username":"invalid","password":"invalid"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "192.0.2.10:" + strconv.Itoa(40000+attempt)
		req.Header.Set("X-Forwarded-For", "198.51.100."+strconv.Itoa(attempt+1))
		w := httptest.NewRecorder()

		handler.Login(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected status %d, got %d", attempt+1, http.StatusUnauthorized, w.Code)
		}
	}

	req := httptest.NewRequest("POST", "/admin/login", bytes.NewBufferString(`{"username":"invalid","password":"invalid"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "192.0.2.10:50000"
	w := httptest.NewRecorder()
	handler.Login(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected status %d after failures from the same IP, got %d", http.StatusTooManyRequests, w.Code)
	}
	assertJSONError(t, w, http.StatusTooManyRequests)
}

func TestAdminHandler_Login_RejectsNewIPWhenTrackingCapacityFull(t *testing.T) {
	handler := newTestAdminHandler(t, service.NewAuthService(memory.NewAdminRepository(), memory.NewSessionRepository()))
	now := time.Now()
	for ip := 0; ip < maxTrackedLoginIPs; ip++ {
		handler.rateLimiter.attempts[strconv.Itoa(ip)] = &attemptRecord{failedCount: 1, lastAttempt: now}
	}
	handler.rateLimiter.nextCleanup = now.Add(time.Hour)

	req := httptest.NewRequest("POST", "/admin/login", strings.NewReader(`{"username":"admin","password":"admin123"}`))
	req.RemoteAddr = "192.0.2.200:1234"
	w := httptest.NewRecorder()
	handler.Login(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusTooManyRequests)
	}
	assertJSONError(t, w, http.StatusTooManyRequests)
}

func TestAdminHandler_Logout(t *testing.T) {
	adminRepo := memory.NewAdminRepository()
	sessionRepo := memory.NewSessionRepository()
	authService := service.NewAuthService(adminRepo, sessionRepo)
	handler := newTestAdminHandler(t, authService)

	token, err := authService.Login(context.Background(), "admin", "admin123")
	if err != nil {
		t.Fatal(err)
	}
	if _, valid := authService.ValidateToken(context.Background(), token); !valid {
		t.Fatal("expected login to create a valid session")
	}

	req := httptest.NewRequest("POST", "/admin/logout", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	w := httptest.NewRecorder()

	handler.Logout(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}
	if _, valid := authService.ValidateToken(context.Background(), token); valid {
		t.Error("expected logout to invalidate the session")
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "admin_token" || cookies[0].MaxAge >= 0 || !cookies[0].HttpOnly {
		t.Errorf("logout cookie = %+v, want expired HttpOnly admin_token", cookies)
	}
}

type failingLogoutSessionRepository struct {
	*memory.SessionRepository
	err error
}

func (r *failingLogoutSessionRepository) Delete(context.Context, string) error {
	return r.err
}

func TestAdminHandler_Logout_InternalError(t *testing.T) {
	sessionRepo := &failingLogoutSessionRepository{
		SessionRepository: memory.NewSessionRepository(),
		err:               errors.New("session store unavailable"),
	}
	authService := service.NewAuthService(memory.NewAdminRepository(), sessionRepo)
	handler := newTestAdminHandler(t, authService)

	req := httptest.NewRequest("POST", "/admin/logout", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: "session-token"})
	w := httptest.NewRecorder()

	handler.Logout(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	assertJSONError(t, w, http.StatusInternalServerError)
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "admin_token" || cookies[0].MaxAge >= 0 {
		t.Errorf("logout cookie = %+v, want expired admin_token despite session error", cookies)
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

type failingAdminRepo struct{}

func (failingAdminRepo) GetByUsername(context.Context, string) (*domain.Admin, error) {
	return nil, errors.New("connection refused")
}

func TestAdminHandler_Login_BackendFailureIsNotCountedAsFailedAttempt(t *testing.T) {
	handler := newTestAdminHandler(t, service.NewAuthService(failingAdminRepo{}, memory.NewSessionRepository()))

	for i := 0; i < 10; i++ {
		req := httptest.NewRequest("POST", "/admin/login", strings.NewReader(`{"username":"a","password":"b"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.Login(w, req)
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("attempt %d: expected 500, got %d", i, w.Code)
		}
		assertJSONError(t, w, http.StatusInternalServerError)
	}
}

func newTestAdminHandler(t *testing.T, authService *service.AuthService) *AdminHandler {
	t.Helper()
	handler := NewAdminHandler(authService)
	t.Cleanup(handler.Stop)
	return handler
}

func TestAdminHandler_Logout_DatabaseUnavailableReturnsServiceUnavailable(t *testing.T) {
	sessionRepo := &failingLogoutSessionRepository{
		SessionRepository: memory.NewSessionRepository(),
		err:               driver.ErrBadConn,
	}
	authService := service.NewAuthService(memory.NewAdminRepository(), sessionRepo)
	handler := newTestAdminHandler(t, authService)

	req := httptest.NewRequest("POST", "/admin/logout", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: "session-token"})
	w := httptest.NewRecorder()

	handler.Logout(w, req)

	assertJSONError(t, w, http.StatusServiceUnavailable, "service_unavailable")
	if got := w.Header().Get("Retry-After"); got != "1" {
		t.Errorf("Retry-After = %q, want 1", got)
	}
}
