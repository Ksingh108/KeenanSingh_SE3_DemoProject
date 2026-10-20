package middleware

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gringottsbankbranchbooking/internal/apierror"
	"github.com/gringottsbankbranchbooking/internal/repository/postgres"
)

func TestRequestObservabilityGeneratesIDAndLogsRoute(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(NewContextHandler(slog.NewJSONHandler(&logs, nil)))
	router := chi.NewRouter()
	router.Use(RequestObservability(logger))
	router.Get("/api/bookings", func(w http.ResponseWriter, r *http.Request) {
		if got := RequestIDFromContext(r.Context()); got == "" {
			t.Error("request ID missing from context")
		}
		w.WriteHeader(http.StatusAccepted)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/bookings?email=alice@example.com&token=private", nil)
	req.Header.Set("X-Request-ID", "client-controlled")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)

	requestID := response.Header().Get("X-Request-ID")
	if requestID == "" || requestID == "client-controlled" {
		t.Fatalf("response request ID = %q, want a generated ID", requestID)
	}
	if _, err := uuid.Parse(requestID); err != nil {
		t.Fatalf("response request ID is not a UUID: %v", err)
	}
	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusAccepted)
	}
	if count := strings.Count(logs.String(), `"request_id":`); count != 1 {
		t.Fatalf("raw log contains %d request_id keys, want exactly one: %s", count, logs.String())
	}

	var entry map[string]any
	if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
		t.Fatalf("decode structured log: %v", err)
	}
	if entry["request_id"] != requestID {
		t.Errorf("logged request ID = %v, want %q", entry["request_id"], requestID)
	}
	if entry["route"] != "/api/bookings" {
		t.Errorf("logged route = %v, want route template", entry["route"])
	}
	if entry["status"] != float64(http.StatusAccepted) {
		t.Errorf("logged status = %v, want %d", entry["status"], http.StatusAccepted)
	}
	if entry["method"] != http.MethodGet {
		t.Errorf("logged method = %v, want %s", entry["method"], http.MethodGet)
	}
	if strings.Contains(logs.String(), "alice@example.com") || strings.Contains(logs.String(), "private") {
		t.Error("structured log contains a raw URL value")
	}
}

func TestRequestObservabilityCorrelatesErrorResponse(t *testing.T) {
	router := chi.NewRouter()
	router.Use(RequestObservability(slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))))
	router.Get("/failure", func(w http.ResponseWriter, r *http.Request) {
		apierror.Write(w, http.StatusBadRequest, "Invalid request")
	})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/failure", nil)
	request.Header.Set("X-Request-ID", "spoofed")
	router.ServeHTTP(response, request)
	var body struct {
		Error struct {
			RequestID string `json:"request_id"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.RequestID == "" || body.Error.RequestID == "spoofed" || body.Error.RequestID != response.Header().Get("X-Request-ID") {
		t.Fatalf("error request ID = %q, headers = %v", body.Error.RequestID, response.Header())
	}
}

func TestContextHandlerAddsRequestIDToHandlerLogs(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(NewContextHandler(slog.NewJSONHandler(&logs, nil)))
	router := chi.NewRouter()
	router.Use(RequestObservability(slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))))
	router.Get("/x", func(w http.ResponseWriter, r *http.Request) {
		logger.ErrorContext(r.Context(), "Operation failed")
	})

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/x", nil))

	var entry map[string]any
	if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
		t.Fatalf("decode structured log: %v", err)
	}
	if entry["request_id"] != response.Header().Get("X-Request-ID") {
		t.Errorf("logged request ID = %v, want %q", entry["request_id"], response.Header().Get("X-Request-ID"))
	}
}

func TestRequestObservabilityCorrelatesRepositoryErrors(t *testing.T) {
	var logs bytes.Buffer
	previousLogger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previousLogger) })
	slog.SetDefault(slog.New(NewContextHandler(slog.NewJSONHandler(&logs, nil))))
	db, err := sql.Open("postgres", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	repo := postgres.NewBookingRepository(db)
	router := chi.NewRouter()
	router.Use(RequestObservability(slog.Default()))
	router.Get("/failure", func(w http.ResponseWriter, r *http.Request) {
		_, err := repo.GetBookedSlots(r.Context(), "branch", "2030-01-01")
		apierror.WriteFailure(w, err, "Failed to check availability")
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/failure", nil))
	foundRepositoryError := false
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatal(err)
		}
		if strings.Count(line, `"request_id":`) != 1 || entry["request_id"] != response.Header().Get("X-Request-ID") {
			t.Fatalf("repository error correlation mismatch: %s", line)
		}
		if entry["operation"] == "get booked slots from database" {
			foundRepositoryError = true
		}
	}
	if !foundRepositoryError {
		t.Fatal("missing repository error log")
	}
}
