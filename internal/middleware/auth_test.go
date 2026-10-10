package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/GringottsBankBranchBooking/internal/repository/memory"
	"github.com/GringottsBankBranchBooking/internal/service"
)

// newAuthFixture returns an AuthService backed by in-memory repositories and a
// valid session token for the seeded admin (ID "admin1").
func newAuthFixture(t *testing.T) (*service.AuthService, string) {
	t.Helper()

	authService := service.NewAuthService(memory.NewAdminRepository(), memory.NewSessionRepository())
	token, err := authService.Login(context.Background(), "admin", "admin123")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	return authService, token
}

func TestRequireAuth_SetsAdminIDInContext(t *testing.T) {
	authService, token := newAuthFixture(t)

	var gotAdminID string
	var gotOK bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAdminID, gotOK = AdminIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/admin/", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	w := httptest.NewRecorder()

	RequireAuth(authService)(next).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d", http.StatusOK, w.Code)
	}
	if !gotOK || gotAdminID != "admin1" {
		t.Errorf("Expected admin ID %q in context, got %q (ok=%v)", "admin1", gotAdminID, gotOK)
	}
}

func TestRequireAuth_RejectsUnauthenticatedRequests(t *testing.T) {
	authService, _ := newAuthFixture(t)

	tests := []struct {
		name   string
		cookie *http.Cookie
	}{
		{"no cookie", nil},
		{"unknown token", &http.Cookie{Name: "admin_token", Value: "not-a-real-token"}},
		{"empty token", &http.Cookie{Name: "admin_token", Value: ""}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nextCalled := false
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalled = true
			})

			req := httptest.NewRequest("GET", "/admin/", nil)
			if tt.cookie != nil {
				req.AddCookie(tt.cookie)
			}
			w := httptest.NewRecorder()

			RequireAuth(authService)(next).ServeHTTP(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
			}
			if nextCalled {
				t.Error("Protected handler must not run for an unauthenticated request")
			}
		})
	}
}

func TestAdminIDFromContext(t *testing.T) {
	if _, ok := AdminIDFromContext(context.Background()); ok {
		t.Error("Expected no admin ID in a bare context")
	}
	if _, ok := AdminIDFromContext(WithAdminID(context.Background(), "")); ok {
		t.Error("An empty admin ID must not count as authenticated")
	}

	adminID, ok := AdminIDFromContext(WithAdminID(context.Background(), "admin1"))
	if !ok || adminID != "admin1" {
		t.Errorf("Expected admin1, got %q (ok=%v)", adminID, ok)
	}
}
