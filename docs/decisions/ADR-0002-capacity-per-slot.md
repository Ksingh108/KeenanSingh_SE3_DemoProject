# ADR-0002: Enforce Capacity per Branch and Time Slot

- **Status:** Accepted for the current demo
- **Date:** 2026-10-17

## Context

Concurrent booking requests must not exceed a branch's configured capacity, including when a slot has no existing bookings to lock.

## Decision

Capacity is the maximum number of non-cancelled bookings for the same branch, date, and exact time-slot key. The PostgreSQL repository checks and inserts in one transaction while locking the branch row. This works for initially empty slots; bookings at the same branch serialize even when they target different slots or dates.

## Consequences

- The capacity invariant is protected against concurrent creates in PostgreSQL.
- Cancelled bookings no longer consume capacity.
- The branch-wide lock favors correctness and a simple invariant over concurrency between unrelated slots. A per-slot lock may be considered if contention becomes material.
- Capacity is not modeled per service, staff member, or resource.