package apierror

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
)

type response struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

// Write emits the shared JSON error envelope used by API handlers and middleware.
func Write(w http.ResponseWriter, statusCode int, message string) {
	WriteWithCode(w, statusCode, codeForStatus(statusCode), message)
}

// WriteWithCode emits the shared JSON error envelope with a stable caller-supplied code.
func WriteWithCode(w http.ResponseWriter, statusCode int, code, message string) {
	write(w, statusCode, code, message)
}

func WriteFailure(w http.ResponseWriter, err error, message string) {
	if isUnavailable(err) {
		w.Header().Set("Retry-After", "1")
		write(w, http.StatusServiceUnavailable, codeForStatus(http.StatusServiceUnavailable), "Service temporarily unavailable. Please try again shortly.")
		return
	}
	write(w, http.StatusInternalServerError, codeForStatus(http.StatusInternalServerError), message)
}

func isUnavailable(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, driver.ErrBadConn) || errors.Is(err, sql.ErrConnDone) {
		return true
	}
	var networkError net.Error
	if errors.As(err, &networkError) {
		return true
	}
	var databaseError interface{ SQLState() string }
	if errors.As(err, &databaseError) {
		code := databaseError.SQLState()
		return strings.HasPrefix(code, "08") || strings.HasPrefix(code, "53") ||
			code == "55P03" || code == "57014" || code == "57P01" || code == "57P02" || code == "57P03"
	}
	return false
}

func write(w http.ResponseWriter, statusCode int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(response{Error: errorBody{
		Code:      code,
		Message:   message,
		RequestID: w.Header().Get("X-Request-ID"),
	}}); err != nil {
		slog.Error("Operation failed", "operation", "encode error response", "error", err.Error(), "request_id", w.Header().Get("X-Request-ID"))
	}
}

func codeForStatus(statusCode int) string {
	switch statusCode {
	case http.StatusBadRequest:
		return "invalid_request"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusConflict:
		return "conflict"
	case http.StatusTooManyRequests:
		return "rate_limited"
	case http.StatusServiceUnavailable:
		return "service_unavailable"
	default:
		if statusCode >= http.StatusInternalServerError {
			return "internal_error"
		}
		return "request_failed"
	}
}
