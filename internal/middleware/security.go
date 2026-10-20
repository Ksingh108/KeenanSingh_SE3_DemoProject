package middleware

import (
	"net/http"
)

// SecurityHeaders adds security headers to responses.
//
// The CSP keeps 'unsafe-inline' (pages use inline scripts/styles) and 'unsafe-eval'
// (the standard Alpine.js build evaluates expressions); all assets are served from
// /static, so no third-party origins are allowed. HSTS is only sent on direct TLS
// connections; behind an HTTPS-terminating proxy the proxy should add it.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval'; style-src 'self' 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		if r.TLS != nil {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}
