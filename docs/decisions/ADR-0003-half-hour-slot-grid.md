# ADR-0003: Use a Fixed Half-Hour Availability Grid

- **Status:** Accepted for the current demo
- **Date:** 2026-10-17

## Context

The availability API needs a finite set of keys to compare against existing bookings. The booking record stores a date and a time-slot string, not a start and end timestamp.

## Decision

Availability exposes the fixed start-time grid from 09:00 through 17:00 in 30-minute increments. Capacity is counted by exact time-slot string. Booking creation accepts only values on that same grid and rejects slots that have already begun. For today's date, availability also marks any slot whose start time is at or before the current business-zone time as unavailable, regardless of remaining capacity. The shared slot policy is used to generate availability and validate booking requests.

## Consequences

- The UI can offer a predictable set of availability choices without modeling service duration.
- The grid is shared across branches and is not derived from each branch's descriptive opening-hours field.
- The 17:00 entry is included as a slot start. Closing-time semantics and overlaps between services are not otherwise defined and should be decided before making the schedule configurable or production-facing.