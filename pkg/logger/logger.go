// Package logger configures the application's standard structured logger.
package logger

import (
	"io"
	"log/slog"
	"regexp"
	"strings"
)

var sanitizeRegex = regexp.MustCompile(`[\x00-\x1f\x7f]|\x1b\[[0-9;]*m`)

// New returns a JSON slog logger that sanitizes string attributes before output.
func New(output io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(output, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			if attr.Value.Kind() == slog.KindString {
				attr.Value = slog.StringValue(SanitizeForLog(attr.Value.String()))
			}
			return attr
		},
	}))
}

// SanitizeForLog removes control characters and limits log string length.
func SanitizeForLog(input string) string {
	sanitized := strings.ReplaceAll(input, "\n", " ")
	sanitized = strings.ReplaceAll(sanitized, "\r", " ")
	sanitized = strings.ReplaceAll(sanitized, "\t", " ")
	sanitized = sanitizeRegex.ReplaceAllString(sanitized, "")
	if len(sanitized) > 200 {
		sanitized = sanitized[:200] + "..."
	}

	return sanitized
}
