# Gringotts Bank Branch Visit Booking System

A Go and PostgreSQL web application for booking branch visits at Gringotts Bank.

## Features

- Anonymous customer booking with branch, service, date, and time-slot selection
- Availability lookup for a selected branch and date
- Branch and service browsing
- Admin dashboard for viewing and cancelling bookings, and adding or archiving branches
- Booking capacity enforced per branch, date and time slot ([ADR-0002](docs/decisions/ADR-0002-capacity-per-slot.md))
- Idempotent booking submission: an optional `Idempotency-Key` header makes retries replay the original booking instead of creating a duplicate

Bookings use 30-minute slots from 09:00 through 17:00. These slots are currently fixed and are not derived from each branch's listed opening hours. Visitors can create bookings; viewing and cancelling bookings requires admin access, and visitors cannot modify bookings through the app.

## Quick Start

### Prerequisites

- Docker Desktop with Docker Compose
- PowerShell on Windows to use `dev.ps1`
- Go 1.26.9 or later for local Go development and tests

The Docker image builds frontend assets with Tailwind v4 and Node 24 Alpine. Local Node/npm is only needed when you are editing or building frontend assets directly.

### Start with the development script

```powershell
.\dev.ps1 docker-restart
```

### Or start with Docker Compose

```powershell
docker compose up --build
```

Open http://localhost:8080 when the app is ready. The Compose setup starts PostgreSQL and seeds sample branches, services, and an admin account on first database initialization. The database volume persists across normal restarts.

The seeded admin credentials are `admin` / `admin123`. They are for local demonstration only; do not expose this deployment publicly with the default credentials.
Docker Compose explicitly selects `APP_ENV=demo`, which applies the demo-only seed migration. Production startup applies schema migrations without demo accounts or sample branches and refuses to start if the demo credential already exists in the database. Do not set `APP_ENV=demo` outside an isolated demo.
Database connections default to `DB_SSL_MODE=verify-full` and production rejects weaker modes. Compose generates a local PostgreSQL CA and server certificate for the isolated demo; the app trusts that CA through a read-only volume. Set `DB_SSL_ROOT_CERT` to your PostgreSQL CA certificate path when deploying elsewhere.

The Go server listens over plain HTTP and does not terminate inbound TLS. Production deployments must put it behind an HTTPS ingress or reverse proxy, prevent external clients from reaching the app port directly, and ensure HTTP requests are rejected or redirected before they reach the application. Admin session cookies are `Secure`, so browser login and authenticated requests require HTTPS. Set `ALLOWED_ORIGINS` to the exact HTTPS origins used by the deployed frontend instead of keeping the localhost defaults. TLS termination at the edge does not change rate-limit identity: the application ignores forwarded client-IP headers, so configure public client-IP and shared-instance limits at that trusted edge as described under Security Features.

### Configuration

The application reads these environment variables. Defaults below apply when a variable is unset; `DB_PASSWORD` has no default and is required.

| Variable | Default | Description |
| --- | --- | --- |
| `APP_ENV` | `production` | Must be `production` or `demo`. Demo mode allows the seeded admin credentials. |
| `PORT` | `8080` | HTTP listen port. |
| `ALLOWED_ORIGINS` | `http://localhost:3000,http://localhost:8080` | Comma-separated allowed browser origins. |
| `READ_TIMEOUT` | `15s` | Maximum HTTP request-read duration. Accepts Go duration syntax or integer seconds. |
| `WRITE_TIMEOUT` | `15s` | Maximum HTTP response-write duration. Accepts Go duration syntax or integer seconds. |
| `IDLE_TIMEOUT` | `60s` | Maximum keep-alive idle duration. Accepts Go duration syntax or integer seconds. |
| `DB_HOST` | `localhost` | PostgreSQL host. |
| `DB_PORT` | `5432` | PostgreSQL port. |
| `DB_USER` | `gringotts` | PostgreSQL user. |
| `DB_PASSWORD` | Required | PostgreSQL password. Compose sets a demo-only value; supply a secret for other deployments. |
| `DB_NAME` | `bookings` | PostgreSQL database name. |
| `DB_SSL_MODE` | `verify-full` | PostgreSQL SSL mode. Production requires `verify-full`. |
| `DB_SSL_ROOT_CERT` | Empty | Path to the PostgreSQL CA certificate when needed. |
| `DB_MAX_OPEN_CONNS` | `20` | Maximum database connections per app process; must be positive. |
| `DB_MAX_IDLE_CONNS` | `5` | Maximum idle database connections; positive and no greater than the open limit. |
| `DB_LOCK_TIMEOUT` | `1s` | PostgreSQL lock-wait limit; must be positive and shorter than the statement timeout. |
| `DB_STATEMENT_TIMEOUT` | `3s` | PostgreSQL per-statement execution limit; must be positive. |

For local Compose settings, see [docker-compose.yml](docker-compose.yml).

### Database Back-Pressure and Health

Size `DB_MAX_OPEN_CONNS` across all replicas: replicas multiplied by the per-process limit must stay below PostgreSQL's connection limit, leaving room for migrations, administration, and other clients. For example, three replicas with 20 connections each leave 40 connections on a database configured for 100. Idle connections expire after one minute and connections are recycled after 30 minutes.

Connection establishment has a two-second timeout. HTTP handlers receive a five-second work budget (shorter when `WRITE_TIMEOUT` requires it), including time waiting for a pool connection. Direct booking repository calls also have a five-second transaction budget. PostgreSQL lock and statement limits apply to every connection, including migrations. Keep those limits below the work budget; large migrations may need a separately planned deployment window.

Database connection failures, resource exhaustion, lock timeouts, and statement/request deadlines return JSON `503` with code `service_unavailable` and `Retry-After: 1` from data handlers. A failed response around commit can have an uncertain outcome: check existing bookings before manually retrying a submission. Retry a submission safely by sending the same `Idempotency-Key` header with the same body: an earlier successful booking is replayed with `201`. If that booking has since been cancelled, the retry returns `409` with code `booking_cancelled`; create a new booking with a new key. Submissions without a key must be checked against existing bookings before retrying.

Compose probes `/live`, which stays `200` during a database outage. Use `/ready` for traffic routing: it checks the database with a two-second deadline, returns JSON `503` while unavailable, and recovers without restarting the app. `/health` remains a detailed database-aware diagnostic endpoint.

The Postgres integration suite includes a failure-under-load test: 40 concurrent bookings against a held branch lock, a slow statement, and a stopped database. It checks bounded failures, pool limits, readiness/liveness, and a successful booking after database restart. Run on a Docker-capable machine or in CI:

```powershell
go test -tags integration ./internal/database -run '^TestDatabaseFailureUnderLoad$' -count=1 -v
```

### Logs and Metrics

Every request receives a server-generated `X-Request-ID`. Context-aware access, handler, service, and repository logs include that ID once per JSON record. API errors include it as `error.request_id`, so customers can quote it. Booking creation logs include `booking_id`, `branch_id`, `date`, and `time_slot`, without customer email or phone. Admin action audit rows retain the request ID in the same transaction as the action; non-HTTP actions may have an empty ID.

`GET /metrics` exposes Prometheus text metrics. Restrict this endpoint at the reverse proxy or ingress to the monitoring network; it is not authenticated by the app. Scraping only reads in-process counters and `db.Stats()` and does not issue database queries, so it remains usable during a database outage.

| Metric | Meaning |
| --- | --- |
| `http_requests_total{method,route,status}` | Completed requests; routes are templates, never raw URLs or query strings. Unknown methods share `OTHER`; unmatched paths share `unmatched`. |
| `http_request_duration_seconds{method,route,status}` | Request latency histogram, including time waiting for the database. |
| `booking_outcomes_total{outcome}` | HTTP booking submissions: `created`, `slot_full`, `idempotency_key_reused`, `booking_cancelled`, `invalid_request`, `branch_not_found`, `rate_limited`, `unavailable`, or `failed`. These describe responses, not guaranteed commit outcomes after connection loss. |
| `go_sql_*{db_name="bookings"}` | Open, in-use, idle and maximum connections; wait counts and duration; connections closed by pool policies. |

Counters reset when the process restarts. Scrape each replica separately. For p95 latency, use `histogram_quantile(0.95, sum by (le, route) (rate(http_request_duration_seconds_bucket[5m])))`. Request IDs, booking IDs, dates, slots, IPs and customer data are never metric labels.

## Use the App

### Visitor booking

1. Open the homepage or the branches page.
2. Choose a branch and service.
3. Select a date and an available time slot.
4. Enter your contact details and submit the booking.

### Admin

Log in from the app using the seeded credentials above. Admins can view bookings, cancel bookings, add branches, and archive branches. Archived branches are hidden from booking but retained with their booking history.

## Architecture & Tech Stack

**Backend**
- Go 1.26+, Chi router, and security middleware
- Layered design: handler → service → repository, with shared domain types; PostgreSQL and in-memory repositories implement the same interfaces
- PostgreSQL for the application; in-memory repositories are used in tests
- Versioned schema migrations with Goose for startup-safe upgrades

**Frontend**
- Tailwind CSS v4 with the CLI build pipeline
- Alpine.js for interactivity

**Development**
- Docker Compose for the application and database
- PowerShell development and test scripts
- Go tests with race detection and optional coverage reports

## Development Commands (dev.ps1)

The `dev.ps1` script provides all development operations:

### Docker Management
```powershell
.\dev.ps1 docker-up        # Start containers
.\dev.ps1 docker-down      # Stop containers
.\dev.ps1 docker-restart   # Stop and rebuild
.\dev.ps1 docker-logs      # View container logs
.\dev.ps1 docker-reset     # Complete reset (removes volumes, rebuilds fresh)
```

### Testing & Quality
```powershell
.\dev.ps1 test             # Run unit tests
.\dev.ps1 test-integration # Run Postgres integration tests (requires Docker)
.\dev.ps1 test-coverage    # Run tests with coverage and HTML reports
```

### Utilities
```powershell
.\dev.ps1 clean            # Clean up reports and Docker
.\dev.ps1 help             # Show all commands
```

## Testing, Reports and CI

```powershell
# Generate test coverage report
.\dev.ps1 test-coverage
```

This creates reports in a timestamped subfolder under `Test Reports/`:
- **test_summary.html** - Pass/fail overview with statistics
- **coverage.html** - Code coverage analysis

### Unit and integration tests

`go test ./...` runs the unit tests against in-memory repositories. They need no
Docker, and include concurrency tests proving a time slot can never be overbooked.

Postgres-backed router responses are checked against the OpenAPI schemas, and
the repositories are covered by integration tests behind the `integration`
build tag. These tests start a throwaway PostgreSQL container
(testcontainers) and apply this project's Goose migrations, so they test the
real schema, constraints and locking, including the same concurrent-booking test
against the real capacity-check transaction. They need a running Docker daemon:

```powershell
.\dev.ps1 test-integration
# or: go test -race -tags integration -count=1 ./cmd/server ./internal/database/... ./internal/repository/postgres/...
```

`-race` needs a C toolchain (cgo). On Windows without one, drop `-race` locally;
CI always runs with it.

### Continuous integration

`.github/workflows/ci.yml` runs on every push and pull request: module checksum
verification, `gofmt`, `go vet`, `govulncheck`, `golangci-lint`, build, tests
with the race detector, the Postgres integration tests, and a Docker image build
that asserts the container does not run as root.

## Security Features

- Input validation and sanitization (allow-list validators for bookings and branches)
- SQL injection prevention (every query is parameterized)
- XSS protection headers
- CORS configuration
- Secure logging (the logger strips control and ANSI characters from string attributes and truncates them to 200 characters, so user input cannot forge log lines)
- Error message sanitization
- Public booking creation: 16 KiB body limit (`413`); 5 attempts per connection IP per 60-second window (`429` with `Retry-After`), counting successful and invalid requests
- Admin login: 16 KiB request body limit (`413`)
- Admin sessions: 256-bit random opaque tokens in `HttpOnly`/`Secure`/`SameSite=Strict` cookies; only the SHA-256 digest of a token is stored, so a leaked database cannot be replayed as live sessions
- Admin audit trail: cancelling a booking and creating or deleting a branch are recorded in `admin_audit_log` with the acting admin's ID (and as a structured log line). A database trigger rejects row updates and deletes; it does not prevent `TRUNCATE` or a privileged user from removing the trigger. Production should restrict those privileges and ship audit records to external storage ([ADR-0008](docs/decisions/ADR-0008-admin-auth-and-audit-log.md)).
- Customer PII: admin booking lookup uses `GET /api/bookings?email=` with `Cache-Control: no-store`; our request log never records the URL. Reverse proxies and CDNs usually log query strings, so redact them for `/api/bookings` ([ADR-0007](docs/decisions/ADR-0007-customer-email-query-parameter.md)).
- CSRF and CORS: there are no CSRF tokens. State-changing admin requests rely on the `SameSite=Strict` session cookie, JSON-only bodies and the CORS allow-list (`ALLOWED_ORIGINS`, credentials enabled, `GET`/`POST`/`DELETE`/`OPTIONS`). Set `ALLOWED_ORIGINS` to exact HTTPS origins you control: any listed origin may make credentialed requests. Never use `*`.
- Container runs as an unprivileged user

Admin login lockout (5 failed attempts within 10 minutes, 15-minute lock) uses the connection IP and is in-memory per process. Tracking is capped at 10,000 IPs; while full, new IPs are rejected with `429` until stale records are cleaned up. Expired records are cleaned up every minute.

Booking throttling is in-memory and per server process; restarts reset quotas. Tracking is capped at 10,000 IPs, with new IPs rejected while full and expired records cleaned up on traffic. The limiter keys only on the connection IP from `RemoteAddr`, ignoring forwarding headers; all visitors behind the same reverse-proxy IP share one bucket of 5 attempts per minute per server process. For public deployments, enforce client-IP and shared multi-instance limits at a trusted edge. Rate limits reduce automated abuse but do not verify customers or prevent distributed junk bookings; stronger protection requires verified contact details or a bot challenge.

### Database schema

Versioned SQL migrations under `migrations/` define the schema; demo seed data lives under `migrations/demo/` and is applied only when `APP_ENV=demo`. The app applies migrations on startup with Goose, using a PostgreSQL session-level advisory lock to serialize concurrent startup migrations. The database enforces the core invariants at the schema boundary, including positive branch capacity, valid booking states, and branch/service availability. Production deployments should grant schema-change privileges deliberately; deployments requiring a least-privilege runtime role should run migrations as a separate deployment step.

For a clean local database, run `docker-reset`; it removes this Compose project's volumes and rebuilds from the migrations. For normal schema changes, add a versioned migration and let the app apply it in order.

## Troubleshooting

- **Login works over HTTP but the session is lost:** the session cookie is `Secure`; use HTTPS (or `localhost`, which browsers treat as secure).
- **Browser CORS errors:** add the exact frontend origin to `ALLOWED_ORIGINS`.
- **`429 Too Many Requests`:** the booking limiter allows 5 requests per minute per connection IP; wait for `Retry-After`. Login locks for 15 minutes after 5 failures. Restarting the process clears both.
- **App exits at startup in production:** `DB_PASSWORD` is required, `DB_SSL_MODE` must be `verify-full`, and the demo admin must not exist in the database.
- **Pages return 404 when running outside Docker:** the server serves `./web/static` relative to its working directory, so start it from the repository root.
- **Stale or broken local database:** run `.\dev.ps1 docker-reset`.

## Project Structure

```
api/                         # OpenAPI contract
cmd/server/                  # Application entry point
docker/                      # PostgreSQL image and initialization
docs/
  decisions/                 # Architecture decision records (ADRs)
internal/
  apierror/ config/ database/ health/ metrics/ middleware/  # Application infrastructure
  domain/ handler/ repository/ service/  # Application layers
migrations/                  # Versioned Goose SQL migrations
pkg/                         # Reusable packages (clock, logger, validator)
web/static/                  # Frontend assets served by the application
.github/workflows/           # CI workflows
```

API contract: [api/openapi.yaml](api/openapi.yaml).

Architecture decisions: [business time zone](docs/decisions/ADR-0001-business-time-zone.md), [capacity per slot](docs/decisions/ADR-0002-capacity-per-slot.md), [half-hour slot grid](docs/decisions/ADR-0003-half-hour-slot-grid.md), [persistence confirms booking](docs/decisions/ADR-0004-persistence-confirms-booking.md), [versioned schema migrations](docs/decisions/ADR-0005-versioned-schema-migrations.md), [public booking request limits](docs/decisions/ADR-0006-public-booking-request-limits.md), [customer email query parameter](docs/decisions/ADR-0007-customer-email-query-parameter.md), [admin sessions and audit log](docs/decisions/ADR-0008-admin-auth-and-audit-log.md), [security headers and CSP](docs/decisions/ADR-0009-security-headers-and-csp.md), and [idempotent booking submission](docs/decisions/ADR-0010-booking-idempotency.md).