# ADR-0010: Idempotent Booking Submission

- **Status:** Accepted for the current demo
- **Date:** 2026-10-20

## Context

A client that loses the response to `POST /api/bookings` cannot tell whether the booking was created. Retrying blindly can consume a second capacity unit ([ADR-0002](ADR-0002-capacity-per-slot.md)); not retrying can lose the booking. Retries are also throttled per IP ([ADR-0006](ADR-0006-public-booking-request-limits.md)).

## Decision

Accept an optional `Idempotency-Key` request header (1-128 visible ASCII characters; otherwise `400`).

- **Scope:** keys are global, not per customer or branch. Clients should use random values such as UUIDs.
- **Fingerprint:** the service hashes (SHA-256) the validated request fields, with the phone number normalized, so reformatting a phone number does not change the fingerprint.
- **Same key, same fingerprint:** replay the stored booking with `201`; no new capacity is consumed.
- **Same key, different fingerprint:** `409 idempotency_key_reused`.
- **Same key, booking since cancelled:** `409 booking_cancelled`. A cancelled booking is never replayed as if it were live; the client must create a new booking with a new key.
- **Concurrency (Postgres):** a transaction-scoped advisory lock on the key serializes concurrent first attempts. A unique partial index on `idempotency_key` and a CHECK that key and fingerprint are both null or both set back this up at the database level. Capacity is still enforced by the branch-row lock.
- **No key:** the request is not idempotent and a retry may create a second booking.
- **Retention:** keys never expire; they live as long as the booking row.

The in-memory repository implements the same rules so tests and the demo behave identically.

## Consequences

- A safe retry after a timeout, `503` or connection loss is possible whenever the client sends a key. Metrics label the two 409 causes separately (`idempotency_key_reused`, `booking_cancelled`) from `slot_full`.
- A global key space means a guessed key can only reveal the replayed booking to someone who also sends an identical request body.
- Keys are retained indefinitely, so storage grows with bookings; the extra columns are small.
- Changing the fingerprint inputs changes stored fingerprints, so existing keys would return `idempotency_key_reused`. Any such change needs a migration or a versioned fingerprint.
- The advisory lock hashes the key to 64 bits; a collision only serializes two unrelated requests briefly and does not affect correctness.

## Revisit When

- Keys need to expire, or to be scoped per customer or API client.
- Idempotency is required for other mutating endpoints (cancellation, admin actions).
- Replaying a cancelled booking should return the cancelled booking (`200`) instead of `409`.
