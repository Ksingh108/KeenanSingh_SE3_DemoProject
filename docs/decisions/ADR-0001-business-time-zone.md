# ADR-0001: Use a Business Time Zone for Booking Dates

- **Status:** Accepted for the current demo
- **Date:** 2026-10-17

## Context

Booking dates and time slots are local wall-clock values. Evaluating a requested date using the server's machine-local zone or UTC could cause different booking outcomes around midnight depending on where the server runs.

## Decision

Booking validation uses `Africa/Johannesburg` as the business time zone. Dates and slots are interpreted in that zone, including when deciding whether a date or slot is in the past. The clock embeds zone data and falls back to a fixed UTC+2 offset if the IANA zone cannot be loaded.

## Consequences

- Booking behavior is independent of the host machine's configured time zone.
- A single business zone is sufficient for the current South African demo.
- Branches in other time zones would need a per-branch zone and corresponding validation and display behavior; that is not modeled today.