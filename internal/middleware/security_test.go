package middleware

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityHeaders(t *testing.T) {
	handler := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	expectedHeaders := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
	}

	if got := w.Header().Get("X-XSS-Protection"); got != "" {
		t.Errorf("X-XSS-Protection is deprecated and should not be set, got %q", got)
	}
	if got := w.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("HSTS must not be sent over plain HTTP, got %q", got)
	}
	if csp := w.Header().Get("Content-Security-Policy"); strings.Contains(csp, "http") {
		t.Errorf("CSP should not allow third-party origins: %s", csp)
	}

	tlsReq := httptest.NewRequest("GET", "/", nil)
	tlsReq.TLS = &tls.ConnectionState{}
	tlsW := httptest.NewRecorder()
	handler.ServeHTTP(tlsW, tlsReq)
	if got := tlsW.Header().Get("Strict-Transport-Security"); got != "max-age=31536000; includeSubDomains" {
		t.Errorf("HSTS over TLS = %q", got)
	}

	for header, expected := range expectedHeaders {
		if got := w.Header().Get(header); got != expected {
			t.Errorf("Header %s = %s, want %s", header, got, expected)
		}
	}
}
