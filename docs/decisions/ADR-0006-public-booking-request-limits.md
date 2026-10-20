# ADR-0006: Bound and Throttle Public Booking Requests

- **Status:** Accepted for the current demo
- **Date:** 2026-10-18

## Context

Anonymous booking creation can consume branch capacity and application resources. Capacity checks prevent overbooking but do not distinguish legitimate bookings from junk. Login throttling does not protect this public endpoint.

Admin login also accepts JSON credentials and must not permit unbounded request bodies.

## Decision

Enforce request limits in the booking creation handler, before validation or persistence:

- Bound the entire request body with `http.MaxBytesReader`, including trailing content and requests without a known content length.
- Count every booking attempt, including successful and invalid requests, against a concurrency-safe, per-IP fixed-window quota.
- Identify clients by their connection IP, ignoring forwarding headers rather than introducing an implicit proxy trust boundary.
- Keep limiter state in memory per server process, cap tracked clients, and clean up expired records on traffic. Reject new clients while tracking capacity is exhausted.

Use a local limiter instead of a shared store to avoid another infrastructure dependency for the current demo. Body and quota limits, response codes, and retry behavior are documented in the [README](../../README.md) and [OpenAPI contract](../../api/openapi.yaml).

Bound admin login request bodies to 16 KiB with `http.MaxBytesReader` and return `413` when the limit is exceeded. This body limit is separate from the public booking quota.

## Consequences

- Oversized and over-quota requests are rejected before they can create bookings.
- Memory use is bounded, but exhausting client tracking can temporarily reject legitimate new clients.
- Restarts reset quotas, and multiple server processes each allow their own quota.
- Users behind a reverse proxy share its connection-IP quota. Forwarding-header spoofing cannot grant a new quota.
- Shared IPs can cause false positives, and fixed windows permit bursts around window boundaries.
- Throttling reduces abuse but does not verify customers or prevent distributed junk bookings from consuming real capacity.

## Revisit When

- Deploying behind a reverse proxy: enforce client-IP limits at a trusted edge, or explicitly define trusted proxies and safe client-IP extraction before using forwarding headers.
- Running multiple instances: enforce a shared quota at the edge or through a shared limiter store.
- Abuse persists within quotas or across IPs: add verified contact details or a bot challenge rather than treating throttling as customer verification.

## Related

- [ADR-0010](ADR-0010-booking-idempotency.md): retries of a throttled booking request are made safe with an `Idempotency-Key`.
