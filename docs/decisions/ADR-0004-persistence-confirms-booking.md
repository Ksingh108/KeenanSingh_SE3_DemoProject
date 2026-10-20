# ADR-0004: Confirm a Booking After Persistence

- **Status:** Accepted
- **Date:** 2026-10-17

## Context

The booking flow needs a clear success condition. The application currently has no email or other customer-notification delivery workflow.

## Decision

A booking is confirmed when the capacity-checked create operation persists successfully. The API response is authoritative, and the UI shows a persistent receipt only after a successful response. A receipt does not claim that an email or message was sent.

## Consequences

- Confirmation reflects the state the system can verify: a persisted booking.
- Notification delivery, retries, and delivery status remain separate future capabilities and are not simulated by this flow.
- A failed create response must not be presented as a confirmed booking.