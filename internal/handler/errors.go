package handler

import (
	"encoding/json"
	"net/http"

	"log/slog"
)

// writeError writes a JSON error response with the specified HTTP status code and message.
// This helper prevents code duplication across error handling in handlers.
// Logs encoding errors to maintain visibility of response failures.
func writeError(w http.ResponseWriter, statusCode int, message string) {
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": message}); err != nil {
		slog.Error("Operation failed", "operation", "encode error response", "error", err.Error())
	}
}
