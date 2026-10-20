package metrics

import (
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/gringottsbankbranchbooking/internal/middleware"
	_ "github.com/lib/pq"
)

func TestMetricsThroughRouter(t *testing.T) {
	db, err := sql.Open("postgres", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})
	db.SetMaxOpenConns(23)
	metrics := New(db)
	router := chi.NewRouter()
	router.Use(middleware.RequestObservability(slog.New(slog.NewJSONHandler(io.Discard, nil)), metrics))
	router.Use(chimiddleware.Recoverer)
	router.Get("/api/bookings/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})
	router.Post("/api/bookings", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})
	router.Get("/panic", func(w http.ResponseWriter, r *http.Request) { panic("test panic") })
	router.With(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		})
	}).Get("/protected/{id}", func(w http.ResponseWriter, r *http.Request) {})
	var done sync.WaitGroup
	for index := 0; index < 25; index++ {
		done.Add(1)
		go func() {
			defer done.Done()
			router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/bookings/private-id?email=private@example.com", nil))
		}()
	}
	done.Wait()
	for _, status := range []int{201, 400, 404, 409, 413, 415, 429, 500, 503} {
		metrics.ObserveRequest(http.MethodPost, "/api/bookings", status, 0, "")
	}
	metrics.ObserveRequest(http.MethodPost, "/api/bookings", 409, 0, "idempotency_key_reused")
	metrics.ObserveRequest(http.MethodPost, "/api/bookings", 409, 0, "booking_cancelled")
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/bookings", nil))
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("CUSTOM", "/missing-secret", nil))
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/protected/private-id", nil))
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/panic", nil))
	response := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("metrics response = %d, headers = %v", response.Code, response.Header())
	}
	for _, want := range []string{
		`http_requests_total{method="GET",route="/api/bookings/{id}",status="202"} 25`,
		`http_request_duration_seconds_count{method="GET",route="/api/bookings/{id}",status="202"} 25`,
		`http_requests_total{method="OTHER",route="unmatched",status="405"} 1`,
		`http_requests_total{method="GET",route="/protected/{id}",status="401"} 1`,
		`http_requests_total{method="GET",route="/panic",status="500"} 1`,
		`booking_outcomes_total{outcome="created"} 2`,
		`booking_outcomes_total{outcome="invalid_request"} 3`,
		`booking_outcomes_total{outcome="slot_full"} 1`,
		`booking_outcomes_total{outcome="branch_not_found"} 1`,
		`booking_outcomes_total{outcome="rate_limited"} 1`,
		`booking_outcomes_total{outcome="failed"} 1`,
		`booking_outcomes_total{outcome="unavailable"} 1`,
		`go_sql_max_open_connections{db_name="bookings"} 23`,
		`go_sql_wait_count_total`,
	} {
		if !strings.Contains(response.Body.String(), want) {
			t.Errorf("missing metric %s", want)
		}
	}
	for _, secret := range []string{"private-id", "private@example.com", "missing-secret", "CUSTOM"} {
		if strings.Contains(response.Body.String(), secret) {
			t.Errorf("metrics contain high-cardinality request value %q", secret)
		}
	}
}
