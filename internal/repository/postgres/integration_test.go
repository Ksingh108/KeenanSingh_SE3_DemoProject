//go:build integration

// Integration tests for the Postgres repositories. They run against a real
// PostgreSQL container created from the project's own init.sql, so they
// exercise the actual schema, constraints and locking rather than mocks.
//
// They need a running Docker daemon and are excluded from the default
// `go test ./...` run. Run them with:
//
//	go test -tags integration ./internal/repository/postgres/...
package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/GringottsBankBranchBooking/internal/domain"
)

// Seed data created by init.sql.
const (
	seededBranchID   = "550e8400-e29b-41d4-a716-446655440001"
	seededBranchID2  = "550e8400-e29b-41d4-a716-446655440002"
	seededBranchID3  = "550e8400-e29b-41d4-a716-446655440003"
	seededServiceID  = "660e8400-e29b-41d4-a716-446655440001"
	seededAdminName  = "admin"
	testSlotDate     = "2030-01-01"
	testSlotTimeSlot = "09:00"
)

// testDB is shared by every integration test in the package.
var testDB *sql.DB

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

// run starts the database, runs the tests and tears the container down. It is
// separate from TestMain so that deferred cleanup runs before os.Exit.
func run(m *testing.M) int {
	ctx := context.Background()

	initScript, err := filepath.Abs(filepath.Join("..", "..", "..", "init.sql"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve init.sql: %v\n", err)
		return 1
	}

	// Same major version as docker-compose.yml.
	container, err := tcpostgres.Run(ctx, "postgres:15-alpine",
		tcpostgres.WithDatabase("gringotts_test"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		tcpostgres.WithInitScripts(initScript),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "start postgres container (is Docker running?): %v\n", err)
		return 1
	}
	defer func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			fmt.Fprintf(os.Stderr, "terminate postgres container: %v\n", err)
		}
	}()

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		fmt.Fprintf(os.Stderr, "connection string: %v\n", err)
		return 1
	}
	testDB, err = sql.Open("postgres", dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open database: %v\n", err)
		return 1
	}
	defer testDB.Close()

	return m.Run()
}

// resetDB returns the mutable tables to the state init.sql leaves them in, so
// tests do not depend on each other or on execution order.
func resetDB(t *testing.T) {
	t.Helper()

	if _, err := testDB.Exec(`TRUNCATE bookings, admin_sessions, admin_audit_log`); err != nil {
		t.Fatalf("truncate tables: %v", err)
	}
	_, err := testDB.Exec(`DELETE FROM branches WHERE id NOT IN ($1, $2, $3)`, seededBranchID, seededBranchID2, seededBranchID3)
	if err != nil {
		t.Fatalf("remove non-seed branches: %v", err)
	}
}

// newSlotBooking returns a valid booking for the seeded branch, on the shared
// test date and slot unless overridden by the caller.
func newSlotBooking() *domain.Booking {
	return &domain.Booking{
		BranchID:     seededBranchID,
		ServiceID:    seededServiceID,
		CustomerName: "Jane Smith",
		Email:        "jane@example.com",
		Phone:        "+27 21 555 0100",
		Date:         testSlotDate,
		TimeSlot:     testSlotTimeSlot,
	}
}

// countBookings counts the non-cancelled bookings for a branch, date and slot
// straight from the table, bypassing the repository under test.
func countBookings(t *testing.T, branchID, date, slot string) int {
	t.Helper()

	var count int
	err := testDB.QueryRow(
		`SELECT COUNT(*) FROM bookings WHERE branch_id = $1 AND date = $2 AND time_slot = $3 AND status != 'cancelled'`,
		branchID, date, slot,
	).Scan(&count)
	if err != nil {
		t.Fatalf("count bookings: %v", err)
	}
	return count
}

// seededAdminID looks up the ID of the admin created by init.sql.
func seededAdminID(t *testing.T) string {
	t.Helper()

	var id string
	if err := testDB.QueryRow(`SELECT id FROM admins WHERE username = $1`, seededAdminName).Scan(&id); err != nil {
		t.Fatalf("look up seeded admin: %v", err)
	}
	return id
}

// utcNow is used instead of time.Now() for timestamps written to TIMESTAMP
// (without time zone) columns, so results do not depend on the host's zone.
func utcNow() time.Time {
	return time.Now().UTC()
}
