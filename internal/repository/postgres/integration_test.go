//go:build integration

// Integration tests for the Postgres repositories. They run against a real
// PostgreSQL container initialized from the project's Goose migrations, so they
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
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/gringottsbankbranchbooking/internal/domain"
)

// Seed data created by the Goose migrations.
const (
	seededBranchID   = "550e8400-e29b-41d4-a716-446655440001"
	seededBranchID2  = "550e8400-e29b-41d4-a716-446655440002"
	seededBranchID3  = "550e8400-e29b-41d4-a716-446655440003"
	seededServiceID  = "660e8400-e29b-41d4-a716-446655440001"
	seededServiceID2 = "660e8400-e29b-41d4-a716-446655440002"
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

	migrationDir, err := filepath.Abs(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve migrations directory: %v\n", err)
		return 1
	}

	demoMigrationDir := filepath.Join(migrationDir, "demo")

	// Same major version as docker-compose.yml.
	container, err := tcpostgres.Run(ctx, "postgres:15-alpine",
		tcpostgres.WithDatabase("gringotts_test"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
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
	defer func() {
		if err := testDB.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "close database: %v\n", err)
		}
	}()
	goose.SetDialect("postgres")
	if err := goose.UpContext(ctx, testDB, migrationDir); err != nil {
		fmt.Fprintf(os.Stderr, "apply migrations: %v\n", err)
		return 1
	}
	// Demo seed data uses its own history table, matching the application's APP_ENV=demo startup.
	goose.SetTableName("demo_goose_db_version")
	err = goose.UpContext(ctx, testDB, demoMigrationDir)
	goose.SetTableName("goose_db_version")
	if err != nil {
		fmt.Fprintf(os.Stderr, "apply demo migrations: %v\n", err)
		return 1
	}

	return m.Run()
}

// resetDB returns the mutable tables to the state the migrations leave them in, so
// tests do not depend on each other or on execution order.
func resetDB(t testing.TB) {
	t.Helper()

	if _, err := testDB.Exec(`TRUNCATE bookings, admin_sessions, admin_audit_log`); err != nil {
		t.Fatalf("truncate tables: %v", err)
	}
	_, err := testDB.Exec(`DELETE FROM branches WHERE id NOT IN ($1, $2, $3)`, seededBranchID, seededBranchID2, seededBranchID3)
	if err != nil {
		t.Fatalf("remove non-seed branches: %v", err)
	}
	if _, err := testDB.Exec(`UPDATE branches SET capacity = CASE id
		WHEN $1 THEN 15 WHEN $2 THEN 8 WHEN $3 THEN 12 END`, seededBranchID, seededBranchID2, seededBranchID3); err != nil {
		t.Fatalf("reset branch capacities: %v", err)
	}
}

func setBranchCapacity(t testing.TB, branchID string, capacity int) {
	t.Helper()
	if _, err := testDB.Exec(`UPDATE branches SET capacity = $1 WHERE id = $2`, capacity, branchID); err != nil {
		t.Fatalf("set branch capacity: %v", err)
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

// seededAdminID looks up the ID of the admin created by the migrations.
func seededAdminID(t *testing.T) string {
	t.Helper()

	var id string
	if err := testDB.QueryRow(`SELECT id FROM admins WHERE username = $1`, seededAdminName).Scan(&id); err != nil {
		t.Fatalf("look up seeded admin: %v", err)
	}
	return id
}

// utcNow provides deterministic UTC instants for integration tests.
func utcNow() time.Time {
	return time.Now().UTC()
}
