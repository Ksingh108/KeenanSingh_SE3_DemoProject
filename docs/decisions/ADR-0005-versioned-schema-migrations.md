# ADR-0005: Use Versioned Postgres Migrations with DB Invariants

- **Status:** Accepted
- **Date:** 2026-10-17

## Context

The application needs a reproducible schema in local Compose, integration tests, and production. Demo seed data must not be installed in production. Business invariants such as positive branch capacity, valid booking states, and service availability at a branch must also hold at the database boundary.

## Decision

Use Goose migrations as the source for the PostgreSQL schema and demo seed data. Apply schema migrations at application startup and in integration tests, and enforce domain invariants in the database. Keep demo seed migrations in a separate directory and apply them only when `APP_ENV=demo`, with a separate migration history table.

The project will:

- keep migration files under `migrations/` and apply them in version order with Goose
- acquire a PostgreSQL session-level advisory lock while applying startup migrations, so concurrent application starts serialize migration work
- use database constraints such as `CHECK (capacity > 0)` and `CHECK (status IN ('confirmed', 'cancelled'))`
- require non-null values for fields the application and API expose as required
- keep the schema migration free of environment-specific seed data
- keep demo branches, services, and admin credentials in a demo-only migration

## Consequences

- A clean production database receives the schema without demo accounts or sample branches.
- A clean demo environment receives the schema and demo seed data consistently when `APP_ENV=demo`.
- Domain invariants are protected even if a bad SQL statement bypasses the application layer.
- Local onboarding remains simple because a clean database is created from the same migrations used at startup.
- Concurrent application starts wait for the active migration run to release its database lock before checking or applying versions.
- Future schema changes use new, versioned migrations rather than ad hoc SQL in application or container setup.
