# ADR-0007: Customer Email as a Query Parameter

- **Status:** Accepted for the current demo
- **Date:** 2026-10-18

## Context

Admins look up a customer's bookings by email. A path-based route such as `GET /api/bookings/{email}` would put the address in the URL, where proxies, CDNs and browser history can record it. The endpoint requires an admin session; the concern is PII in infrastructure logs, not public exposure.

## Decision

- Look up bookings with `GET /api/bookings?email=...`, behind `RequireAuth`, with the same pagination as the unfiltered list.
- Send `Cache-Control: no-store` on booking list responses, because they contain customer PII.
- Keep the application request log free of URL values. It records the route template only, and a test enforces this.
- Document that proxies and CDNs must redact query strings for `/api/bookings` in the [README](../../README.md) and the [OpenAPI contract](../../api/openapi.yaml).

Rejected for the current demo: `POST /api/bookings/search` with the email in the body would keep PII out of URLs, but would introduce a separate search operation instead of filtering the booking collection. Since booking-list responses use `Cache-Control: no-store`, cacheability is not a deciding factor. The remaining infrastructure-log risk is accepted with query-string redaction; revisit this choice if policy no longer permits PII in URLs.

## Consequences

- The address does not appear in the path, so path-based route logs and metrics are clean.
- Query strings are still logged by many proxies and CDNs; this decision relies on operators redacting them.
- Responses are never cached by browsers or shared caches.

## Revisit When

- A policy such as POPIA or an internal bank standard prohibits PII in URLs: move to a POST search with the email in the body.
- A trusted edge is deployed: confirm its access-log format excludes query strings for this path.
