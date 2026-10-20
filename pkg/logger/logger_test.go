package logger

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestSanitizeForLog(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"normal text", "normal text"},
		{"text\nwith\nnewlines", "text with newlines"},
		{"text\rwith\rcarriage", "text with carriage"},
		{"text\twith\ttabs", "text with tabs"},
		{strings.Repeat("a", 250), strings.Repeat("a", 200) + "..."},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := SanitizeForLog(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeForLog(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNew_SanitizesStructuredStringAttributes(t *testing.T) {
	var buf bytes.Buffer
	logger := New(&buf)
	logger.Info("admin action", "target_id", "42\n{\"level\":\"ERROR\",\"message\":\"forged\"}")

	output := strings.TrimSuffix(buf.String(), "\n")
	if strings.Contains(output, "\n") {
		t.Fatalf("Expected one JSON log line, got %q", buf.String())
	}

	var entry map[string]any
	if err := json.Unmarshal([]byte(output), &entry); err != nil {
		t.Fatalf("Expected a JSON log line, got %q: %v", output, err)
	}
	if entry["msg"] != "admin action" {
		t.Errorf("Unexpected message: %v", entry["msg"])
	}
	if entry["target_id"] != `42 {"level":"ERROR","message":"forged"}` {
		t.Errorf("Unexpected target_id: %v", entry["target_id"])
	}
}
