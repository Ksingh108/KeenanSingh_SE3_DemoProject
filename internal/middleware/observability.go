package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	applog "github.com/gringottsbankbranchbooking/pkg/logger"
)

const requestIDHeader = "X-Request-ID"

// RequestIDFromContext returns the server-generated ID for the current request.
func RequestIDFromContext(ctx context.Context) string {
	return applog.RequestIDFromContext(ctx)
}

// contextHandler adds the request ID to every record logged with a request context.
type contextHandler struct{ slog.Handler }

// NewContextHandler wraps h so that slog.*Context calls made with a request's
// context include its request_id.
func NewContextHandler(h slog.Handler) slog.Handler { return contextHandler{h} }

func (h contextHandler) Handle(ctx context.Context, record slog.Record) error {
	if id := RequestIDFromContext(ctx); id != "" {
		record.AddAttrs(slog.String("request_id", id))
	}
	return h.Handler.Handle(ctx, record)
}

func (h contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return contextHandler{h.Handler.WithAttrs(attrs)}
}

func (h contextHandler) WithGroup(name string) slog.Handler {
	return contextHandler{h.Handler.WithGroup(name)}
}

type outcomeKey struct{}

// SetOutcome lets a handler refine the metrics outcome of its response when the
// HTTP status alone is ambiguous (for example, the three 409 booking causes).
// It is a no-op outside RequestObservability.
func SetOutcome(ctx context.Context, outcome string) {
	if slot, ok := ctx.Value(outcomeKey{}).(*string); ok {
		*slot = outcome
	}
}

type requestObserver interface {
	ObserveRequest(method, route string, status int, duration time.Duration, outcome string)
}

// RequestObservability adds a server-generated request ID and structured access log.
func RequestObservability(logger *slog.Logger, observers ...requestObserver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := uuid.NewString()
			w.Header().Set(requestIDHeader, requestID)
			ctx := applog.WithRequestID(r.Context(), requestID)
			var outcome string
			ctx = context.WithValue(ctx, outcomeKey{}, &outcome)
			r = r.WithContext(ctx)

			wrapped := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
			started := time.Now()
			next.ServeHTTP(wrapped, r)

			status := wrapped.Status()
			if status == 0 {
				status = http.StatusOK
			}
			route := chi.RouteContext(r.Context()).RoutePattern()
			if route == "" {
				route = "unmatched"
			}
			duration := time.Since(started)
			for _, observer := range observers {
				observer.ObserveRequest(r.Method, route, status, duration, outcome)
			}
			logger.InfoContext(r.Context(), "HTTP request completed",
				"method", r.Method,
				"route", route,
				"status", status,
				"duration", duration,
			)
		})
	}
}
