package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/gringottsbankbranchbooking/internal/repository/memory"
	"github.com/gringottsbankbranchbooking/internal/service"
)

func TestBranchHandler_GetBranches(t *testing.T) {
	repo := memory.NewBranchRepository()
	svc := service.NewBranchService(repo)
	handler := NewBranchHandler(svc, newTestAdminActionService())

	req := httptest.NewRequest("GET", "/branches", nil)
	w := httptest.NewRecorder()

	handler.GetBranches(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestBranchHandler_CreateBranch(t *testing.T) {
	repo := memory.NewBranchRepository()
	svc := service.NewBranchService(repo)
	handler := NewBranchHandler(svc, newTestAdminActions(memory.NewBookingRepository("empty"), repo))

	body := `{"name":"Test Branch","city":"Test City","address":"123 Test St","phone":"082 123 4567","capacity":10,"services":["660e8400-e29b-41d4-a716-446655440001"]}`
	req := withAdmin(httptest.NewRequest("POST", "/admin/branches", bytes.NewBufferString(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.CreateBranch(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected status %d, got %d", http.StatusCreated, w.Code)
	}
}

func TestBranchHandler_CreateBranch_UnknownService(t *testing.T) {
	repo := memory.NewBranchRepository()
	handler := NewBranchHandler(service.NewBranchService(repo), newTestAdminActions(memory.NewBookingRepository("empty"), repo))
	body := `{"name":"Test Branch","city":"Test City","address":"123 Test St","phone":"082 123 4567","capacity":10,"services":["660e8400-e29b-41d4-a716-446655440099"]}`
	req := withAdmin(httptest.NewRequest("POST", "/admin/branches", bytes.NewBufferString(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.CreateBranch(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	assertJSONError(t, w, http.StatusBadRequest)
}

func TestBranchHandler_CreateBranch_RequiresJSONContentType(t *testing.T) {
	repo := memory.NewBranchRepository()
	handler := NewBranchHandler(service.NewBranchService(repo), newTestAdminActionService())
	req := withAdmin(httptest.NewRequest("POST", "/admin/branches", strings.NewReader(`{}`)))
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()

	handler.CreateBranch(w, req)

	assertJSONError(t, w, http.StatusUnsupportedMediaType)
}

func TestBranchHandler_CreateBranch_RejectsOversizedBody(t *testing.T) {
	repo := memory.NewBranchRepository()
	handler := NewBranchHandler(service.NewBranchService(repo), newTestAdminActions(memory.NewBookingRepository("empty"), repo))

	body := `{"name":"Test Branch","city":"Test City","address":"123 Test St","phone":"082 123 4567","capacity":10,"services":["660e8400-e29b-41d4-a716-446655440001"]}` + strings.Repeat(" ", maxBranchBodyBytes)
	req := withAdmin(httptest.NewRequest("POST", "/admin/branches", bytes.NewBufferString(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.CreateBranch(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("Expected status %d, got %d", http.StatusRequestEntityTooLarge, w.Code)
	}
}

func TestBranchHandler_CreateBranch_Validation(t *testing.T) {
	const serviceID = "660e8400-e29b-41d4-a716-446655440001"

	tests := []struct {
		name string
		body string
	}{
		{"malformed json", `{"name":`},
		{"multiple JSON values", `{"name":"Test Branch","city":"Cape Town","address":"1 Long St","phone":"021 555 0100","capacity":10,"services":["` + serviceID + `"]} {}`},
		{"missing name", `{"city":"Cape Town","address":"1 Long St","phone":"021 555 0100","capacity":10,"services":["` + serviceID + `"]}`},
		{"whitespace-only name", `{"name":"   ","city":"Cape Town","address":"1 Long St","phone":"021 555 0100","capacity":10,"services":["` + serviceID + `"]}`},
		{"name too long", `{"name":"` + strings.Repeat("a", 256) + `","city":"Cape Town","address":"1 Long St","phone":"021 555 0100","capacity":10,"services":["` + serviceID + `"]}`},
		{"name with markup", `{"name":"<script>alert(1)</script>","city":"Cape Town","address":"1 Long St","phone":"021 555 0100","capacity":10,"services":["` + serviceID + `"]}`},
		{"name with control characters", `{"name":"Branch\nINFO fake log line","city":"Cape Town","address":"1 Long St","phone":"021 555 0100","capacity":10,"services":["` + serviceID + `"]}`},
		{"missing city", `{"name":"Test Branch","address":"1 Long St","phone":"021 555 0100","capacity":10,"services":["` + serviceID + `"]}`},
		{"city with digits", `{"name":"Test Branch","city":"City42","address":"1 Long St","phone":"021 555 0100","capacity":10,"services":["` + serviceID + `"]}`},
		{"missing address", `{"name":"Test Branch","city":"Cape Town","phone":"021 555 0100","capacity":10,"services":["` + serviceID + `"]}`},
		{"address too long", `{"name":"Test Branch","city":"Cape Town","address":"` + strings.Repeat("a", 501) + `","phone":"021 555 0100","capacity":10,"services":["` + serviceID + `"]}`},
		{"missing phone", `{"name":"Test Branch","city":"Cape Town","address":"1 Long St","capacity":10,"services":["` + serviceID + `"]}`},
		{"phone with invalid characters", `{"name":"Test Branch","city":"Cape Town","address":"1 Long St","phone":"021;DROP","capacity":10,"services":["` + serviceID + `"]}`},
		{"zero capacity", `{"name":"Test Branch","city":"Cape Town","address":"1 Long St","phone":"021 555 0100","capacity":0,"services":["` + serviceID + `"]}`},
		{"negative capacity", `{"name":"Test Branch","city":"Cape Town","address":"1 Long St","phone":"021 555 0100","capacity":-5,"services":["` + serviceID + `"]}`},
		{"capacity above maximum", `{"name":"Test Branch","city":"Cape Town","address":"1 Long St","phone":"021 555 0100","capacity":1001,"services":["` + serviceID + `"]}`},
		{"no services", `{"name":"Test Branch","city":"Cape Town","address":"1 Long St","phone":"021 555 0100","capacity":10,"services":[]}`},
		{"malformed service id", `{"name":"Test Branch","city":"Cape Town","address":"1 Long St","phone":"021 555 0100","capacity":10,"services":["abc'; --"]}`},
		{"duplicate service id", `{"name":"Test Branch","city":"Cape Town","address":"1 Long St","phone":"021 555 0100","capacity":10,"services":["` + serviceID + `","` + serviceID + `"]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := memory.NewBranchRepository()
			handler := NewBranchHandler(service.NewBranchService(repo), newTestAdminActionService())

			req := withAdmin(httptest.NewRequest("POST", "/admin/branches", bytes.NewBufferString(tt.body)))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handler.CreateBranch(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("Expected status %d, got %d (body: %q)", http.StatusBadRequest, w.Code, w.Body.String())
			}
			assertJSONError(t, w, http.StatusBadRequest)

			// A rejected request must not have created anything.
			branches, err := repo.GetAll(req.Context())
			if err != nil {
				t.Fatalf("GetAll failed: %v", err)
			}
			if len(branches) != 3 {
				t.Errorf("Expected only the 3 seeded branches, got %d", len(branches))
			}
		})
	}
}

// newBranchRouter wires DeleteBranch into a chi router so that the {id} route
// parameter is populated exactly as it is in production.
func newBranchRouter(handler *BranchHandler) http.Handler {
	r := chi.NewRouter()
	r.Delete("/admin/branches/{id}", handler.DeleteBranch)
	return r
}

func TestBranchHandler_DeleteBranch(t *testing.T) {

	tests := []struct {
		name         string
		path         string
		expectedCode int
	}{
		{"existing branch", "/admin/branches/" + seededBranchID, http.StatusNoContent},
		{"unknown branch", "/admin/branches/770e8400-e29b-41d4-a716-4466554400ff", http.StatusNotFound},
		{"malformed id", "/admin/branches/abc'%3B--", http.StatusBadRequest},
		{"id too long", "/admin/branches/" + strings.Repeat("a", 51), http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := memory.NewBranchRepository()
			router := newBranchRouter(NewBranchHandler(service.NewBranchService(repo), newTestAdminActions(memory.NewBookingRepository("empty"), repo)))

			req := withAdmin(httptest.NewRequest("DELETE", tt.path, nil))
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if w.Code != tt.expectedCode {
				t.Errorf("Expected status %d, got %d (body: %q)", tt.expectedCode, w.Code, w.Body.String())
			}
			if tt.expectedCode >= http.StatusBadRequest {
				assertJSONError(t, w, tt.expectedCode)
			}
		})
	}
}

func TestBranchHandler_DeleteBranch_RemovesBranch(t *testing.T) {

	repo := memory.NewBranchRepository()
	router := newBranchRouter(NewBranchHandler(service.NewBranchService(repo), newTestAdminActions(memory.NewBookingRepository("empty"), repo)))

	req := withAdmin(httptest.NewRequest("DELETE", "/admin/branches/"+seededBranchID, nil))
	router.ServeHTTP(httptest.NewRecorder(), req)

	if _, err := repo.GetByID(req.Context(), seededBranchID); err == nil {
		t.Error("Expected branch to be gone after delete")
	}

	// Deleting it a second time is now a 404, not a silent 204.
	w := httptest.NewRecorder()
	router.ServeHTTP(w, withAdmin(httptest.NewRequest("DELETE", "/admin/branches/"+seededBranchID, nil)))
	if w.Code != http.StatusNotFound {
		t.Errorf("Expected second delete to return %d, got %d", http.StatusNotFound, w.Code)
	}
}

// Calling the handler directly with a path that does not match the route used
// to panic on a slice-bounds error. Without a route parameter it must now
// return a clean 400.
func TestBranchHandler_DeleteBranch_NoRouteParam(t *testing.T) {
	handler := NewBranchHandler(service.NewBranchService(memory.NewBranchRepository()), newTestAdminActionService())

	req := withAdmin(httptest.NewRequest("DELETE", "/x", nil))
	w := httptest.NewRecorder()

	handler.DeleteBranch(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}
