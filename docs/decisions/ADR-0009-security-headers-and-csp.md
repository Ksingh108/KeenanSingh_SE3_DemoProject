# ADR-0009: Security Headers and Content Security Policy

- **Status:** Accepted for the current demo
- **Date:** 2026-10-19

## Context

The app serves its own HTML, CSS and JavaScript and calls only its own API. Its pages use inline scripts and styles, and Alpine's standard build evaluates expressions at runtime.

## Decision

- Serve all assets from `/static`. No CDN origins are in the CSP.
- CSP: `default-src 'self'`, plus `connect-src 'self'`, `frame-ancestors 'none'`, `base-uri 'self'` and `form-action 'self'`.
- Keep `'unsafe-inline'` and `'unsafe-eval'` in `script-src`, and `'unsafe-inline'` in `style-src`, because the current pages need them.
- Send `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY` and `Referrer-Policy: strict-origin-when-cross-origin`. Do not send the deprecated `X-XSS-Protection`.
- Send HSTS only when the request arrived over TLS. Behind an HTTPS ingress the app sees plain HTTP, so the ingress must add HSTS.
- CORS allows only the origins listed in `ALLOWED_ORIGINS` (defaults to the local dev origins). Production must set it to the real site origin.

## Consequences

- Injected script is still not blocked by the CSP, so input validation and output escaping remain the main XSS defence.
- Framing, base-tag injection and form hijacking are blocked.

## Revisit When

- The pages can be refactored: move inline scripts into files, adopt Alpine's CSP build, then drop `'unsafe-inline'` and `'unsafe-eval'`.
