package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gringottsbankbranchbooking/internal/apierror"
)

func TestRequestDeadline(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/branches", nil)
	result := httptest.NewRecorder()
	handler := RequestDeadline(10 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		apierror.WriteFailure(w, r.Context().Err(), "Failed to get branches")
	}))
	start := time.Now()
	handler.ServeHTTP(result, request)
	if time.Since(start) > time.Second {
		t.Fatal("request exceeded its budget")
	}
	if result.Code != http.StatusServiceUnavailable || result.Header().Get("Retry-After") != "1" {
		t.Fatalf("response = %d, headers = %v", result.Code, result.Header())
	}
	if strings.Count(result.Body.String(), `"error"`) != 1 {
		t.Fatalf("expected exactly one error response, got %s", result.Body)
	}
}

func TestRequestDeadlinePreservesEarlierDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	want, _ := ctx.Deadline()
	handler := RequestDeadline(5 * time.Second)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := r.Context().Deadline()
		if !ok || !got.Equal(want) {
			t.Fatalf("deadline = %v, want %v", got, want)
		}
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx))
}
