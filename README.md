# Gringotts Bank Branch Visit Booking System

A Go and PostgreSQL web application for booking branch visits at Gringotts Bank.

## Features

- Anonymous customer booking with branch, service, date, and time-slot selection
- Availability lookup for a selected branch and date
- Branch and service browsing
- Admin dashboard for viewing and cancelling bookings, and adding or deleting branches
- Per-branch booking capacity checks

Bookings use 30-minute slots from 09:00 through 17:00. These slots are currently fixed and are not derived from each branch's listed opening hours. Visitors can create bookings; viewing and cancelling bookings requires admin access, and visitors cannot modify bookings through the app.

## Quick Start

### Prerequisites

- Docker Desktop with Docker Compose
- PowerShell on Windows to use `dev.ps1`
- Go 1.25.2 or later for local Go development and tests

Node.js is not needed to run the Docker image; the image builds the Tailwind CSS assets.

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

## Use the App

### Visitor booking

1. Open the homepage or the branches page.
2. Choose a branch and service.
3. Select a date and an available time slot.
4. Enter your contact details and submit the booking.

### Admin

Log in from the app using the seeded credentials above. Admins can view bookings, cancel bookings, and add or delete branches.

## Architecture & Tech Stack

**Backend**
- Go 1.25.2, Chi router, and security middleware
- Clean Architecture with layered separation
- PostgreSQL for the application; in-memory repositories are used in tests

**Frontend**
- Tailwind CSS for styling
- Alpine.js for interactivity

**Development**
- Docker Compose for the application and database
- PowerShell development and test scripts
- Go tests with race detection and optional coverage reports

## 🛠️ Development Commands (dev.ps1)

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

## 📊 Testing & Reports

The system includes comprehensive testing with detailed reporting:

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

The Postgres repositories are covered by integration tests behind the
`integration` build tag. They start a throwaway PostgreSQL container
(testcontainers) from this project's own `init.sql`, so they test the real
schema, constraints and locking, including the same concurrent-booking test
against the real capacity-check transaction. They need a running Docker daemon:

```powershell
.\dev.ps1 test-integration
# or: go test -race -tags integration ./internal/repository/postgres/...
```

`-race` needs a C toolchain (cgo). On Windows without one, drop `-race` locally;
CI always runs with it.

### Continuous integration

`.github/workflows/ci.yml` runs on every push and pull request: module checksum
verification, `gofmt`, `go vet`, build, tests with the race detector, the
Postgres integration tests, and a Docker image build that asserts the container
does not run as root.

## 🔒 Security Features

- Input validation and sanitization (allow-list validators for bookings and branches)
- SQL injection prevention (every query is parameterized)
- XSS protection headers
- CORS configuration
- Secure logging (prevents log injection)
- Error message sanitization
- Admin sessions: 256-bit random opaque tokens in `HttpOnly`/`Secure`/`SameSite=Strict` cookies; only the SHA-256 digest of a token is stored, so a leaked database cannot be replayed as live sessions
- Admin audit trail: cancelling a booking and creating or deleting a branch are recorded in `admin_audit_log` with the acting admin's ID (and as a structured log line); the table is append-only for the application
- Container runs as an unprivileged user

### Upgrading an existing database

`init.sql` only runs when the database volume is first created. The session table
now stores `token_hash` instead of `token` and there is a new `admin_audit_log`
table, so an existing development database needs `.\dev.ps1 docker-reset`. Existing
admin sessions are invalidated by this change and admins simply log in again.

## 📁 Project Structure

```
cmd/           # Application entry points
internal/      # Private application code
  domain/      # Business entities and rules
  repository/  # Data access layer
  service/     # Business logic layer
  handler/     # HTTP handlers
pkg/           # Reusable packages
web/           # Frontend assets
```