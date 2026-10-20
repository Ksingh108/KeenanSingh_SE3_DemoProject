package metrics

import (
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	registry *prometheus.Registry
	requests *prometheus.CounterVec
	latency  *prometheus.HistogramVec
	bookings *prometheus.CounterVec
}

func New(db *sql.DB) *Metrics {
	metrics := &Metrics{
		registry: prometheus.NewRegistry(),
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total", Help: "Completed HTTP requests by method, route template, and status.",
		}, []string{"method", "route", "status"}),
		latency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "http_request_duration_seconds", Help: "HTTP request duration by method, route template, and status.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "route", "status"}),
		bookings: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "booking_outcomes_total", Help: "Booking submission outcomes, including validation and throttling failures.",
		}, []string{"outcome"}),
	}
	metrics.registry.MustRegister(metrics.requests, metrics.latency, metrics.bookings,
		collectors.NewDBStatsCollector(db, "bookings"))
	return metrics
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// ObserveRequest records one completed request. A non-empty outcome overrides the
// status-derived booking outcome label.
func (m *Metrics) ObserveRequest(method, route string, status int, duration time.Duration, outcome string) {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodOptions, http.MethodConnect, http.MethodTrace:
	default:
		method = "OTHER"
	}
	statusLabel := strconv.Itoa(status)
	m.requests.WithLabelValues(method, route, statusLabel).Inc()
	m.latency.WithLabelValues(method, route, statusLabel).Observe(duration.Seconds())
	if method == http.MethodPost && route == "/api/bookings" {
		if outcome == "" {
			outcome = bookingOutcome(status)
		}
		m.bookings.WithLabelValues(outcome).Inc()
	}
}

func bookingOutcome(status int) string {
	switch status {
	case http.StatusCreated:
		return "created"
	case http.StatusConflict:
		return "slot_full" // default for 409; handlers refine via middleware.SetOutcome
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnsupportedMediaType:
		return "invalid_request"
	case http.StatusNotFound:
		return "branch_not_found"
	case http.StatusTooManyRequests:
		return "rate_limited"
	case http.StatusServiceUnavailable:
		return "unavailable"
	default:
		return "failed"
	}
}
