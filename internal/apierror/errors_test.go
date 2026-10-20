package apierror

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lib/pq"
)

func TestWrite(t *testing.T) {
	tests := []struct {
		status int
		code   string
	}{
		{http.StatusBadRequest, "invalid_request"},
		{http.StatusUnauthorized, "unauthorized"},
		{http.StatusNotFound, "not_found"},
		{http.StatusConflict, "conflict"},
		{http.StatusTooManyRequests, "rate_limited"},
		{http.StatusInternalServerError, "internal_error"},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			result := httptest.NewRecorder()
			Write(result, tt.status, "request failed")

			var body response
			if err := json.Unmarshal(result.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if result.Code != tt.status || body.Error.Code != tt.code || body.Error.Message != "request failed" {
				t.Errorf("response = status %d, %+v; want status %d and code %q", result.Code, body.Error, tt.status, tt.code)
			}
		})
	}
}

func TestWriteWithCode(t *testing.T) {
	result := httptest.NewRecorder()
	WriteWithCode(result, http.StatusConflict, "slot_full", "Branch capacity exceeded")

	var body response
	if err := json.Unmarshal(result.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result.Code != http.StatusConflict || body.Error.Code != "slot_full" {
		t.Fatalf("response = status %d, %+v; want status %d and code %q", result.Code, body.Error, http.StatusConflict, "slot_full")
	}
}

func TestWriteFailure(t *testing.T) {
	for _, failure := range []error{
		context.DeadlineExceeded,
		driver.ErrBadConn,
		&net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")},
		&pq.Error{Code: "08006"},
		&pq.Error{Code: "53300"},
		&pq.Error{Code: "55P03"},
		&pq.Error{Code: "57014"},
		&pq.Error{Code: "57P01"},
		&pq.Error{Code: "57P02"},
		&pq.Error{Code: "57P03"},
	} {
		t.Run(failure.Error(), func(t *testing.T) {
			result := httptest.NewRecorder()
			WriteFailure(result, fmt.Errorf("repository: %w", failure), "Failed to create booking")
			if result.Code != http.StatusServiceUnavailable || result.Header().Get("Retry-After") != "1" {
				t.Fatalf("response = %d, headers = %v", result.Code, result.Header())
			}
			if !strings.Contains(result.Body.String(), `"code":"service_unavailable"`) {
				t.Fatalf("unexpected error response: %s", result.Body)
			}
		})
	}
	for _, failure := range []error{errors.New("unexpected failure"), &pq.Error{Code: "23505"}} {
		result := httptest.NewRecorder()
		WriteFailure(result, failure, "Failed to create booking")
		if result.Code != http.StatusInternalServerError || result.Header().Get("Retry-After") != "" {
			t.Fatalf("non-transient response = %d, headers = %v", result.Code, result.Header())
		}
	}
}
