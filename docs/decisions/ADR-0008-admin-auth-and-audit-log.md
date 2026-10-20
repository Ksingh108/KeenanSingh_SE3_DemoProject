# ADR-0008: Admin Sessions and Append-Only Audit Log

- **Status:** Accepted for the current demo
- **Date:** 2026-10-19

## Context

Admins manage branches and bookings. Their actions must be attributable, and credentials and sessions must be hard to abuse.

## Decision

- **Login:** username and password, checked with bcrypt. Unknown usernames do the same bcrypt work as known ones, so response time does not reveal valid usernames. Failures return a generic 401.
- **Lockout:** 5 failed attempts within 10 minutes lock the client IP for 15 minutes. State is in-memory and per process, keyed on the connection IP only ([ADR-0006](ADR-0006-public-booking-request-limits.md)). Tracking is capped at 10,000 IPs; while full, new IPs are rejected until expired records are cleaned up every minute.
- **Session:** a random token in the `admin_token` cookie (`HttpOnly`, `Secure`, `SameSite=Strict`, 24 hours). Only its hash is stored in `admin_sessions`. Logout deletes the row.
- **CSRF:** `SameSite=Strict` plus an allow-listed CORS policy (`ALLOWED_ORIGINS`, see [ADR-0009](ADR-0009-security-headers-and-csp.md)). No separate CSRF token.
- **Audit log:** each admin action writes a row to `admin_audit_log`. The application only inserts. The schema migration installs a trigger that rejects `UPDATE` and `DELETE`, so history cannot be edited even by a bad query. `admin_id` uses `ON DELETE RESTRICT`, so an admin with history cannot be removed.
- **Request correlation:** HTTP admin actions persist the server-generated request ID atomically with their audit row and include it in structured logs. Actions outside HTTP may have an empty request ID.
- **Branches:** deleting a branch is a soft delete (`is_active = FALSE`), so bookings and audit rows keep a valid reference. The booking-to-branch-service foreign key also restricts deleting a service association while bookings reference it, preventing a cascade from erasing booking history.

## Consequences

- Audit history is tamper-resistant against application bugs and ordinary SQL.
- The trigger does not stop `TRUNCATE` or a superuser who drops the trigger. Production should run the app as a role without those rights and ship the log to external storage.
- Lockout resets on restart and is not shared between replicas.
- A full tracking map temporarily rejects new client IPs, including legitimate admins behind newly observed addresses.
- There is no password change or rotation flow yet.

## Revisit When

- The app runs as more than one replica: move lockout state to a shared store.
- Auditors need stronger guarantees: ship audit rows to write-once external storage.
