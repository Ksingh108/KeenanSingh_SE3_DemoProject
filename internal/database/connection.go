package database

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/gringottsbankbranchbooking/internal/config"
	_ "github.com/lib/pq"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

// NewConnection establishes a connection to the PostgreSQL database using the provided config.
// It respects all config settings including SSLMode and validates credentials against the config.
// The function uses exponential backoff to handle database startup delays in containerized environments.
//
// Parameters:
//   - dbConfig: DatabaseConfig struct containing host, port, user, password, name, and sslmode
//
// Returns:
//   - *sql.DB: Connection pool ready for queries
//   - error: Connection or validation error
func NewConnection(dbConfig *config.DatabaseConfig, appEnvironment string) (*sql.DB, error) {
	connStr := buildConnectionString(dbConfig)

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, err
	}
	configurePool(db, dbConfig)

	// Wait for DB to be ready using per-ping context timeout and exponential backoff
	// Each ping will have a short timeout so a single slow Ping won't block the whole loop.
	maxAttempts := 30
	backoff := 500 * time.Millisecond
	maxBackoff := 5 * time.Second

	for i := 0; i < maxAttempts; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err = db.PingContext(ctx)
		cancel()

		if err == nil {
			if err := runMigrations(db, appEnvironment); err != nil {
				if closeErr := db.Close(); closeErr != nil {
					fmt.Printf("close database connection after migration failure: %v\n", closeErr)
				}
				return nil, err
			}
			slog.Info("Successfully connected to database")
			return db, nil
		}

		slog.Info("Waiting for database", "attempt", i+1, "max_attempts", maxAttempts, "error", err.Error())

		time.Sleep(backoff)
		backoff = backoff * 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}

	if closeErr := db.Close(); closeErr != nil {
		slog.Error("Operation failed", "operation", "close database connection after failed startup ping", "error", closeErr.Error())
	}

	return nil, fmt.Errorf("failed to connect to database after 30 attempts: %v", err)
}

func buildConnectionString(dbConfig *config.DatabaseConfig) string {
	connectionURL := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(dbConfig.User, dbConfig.Password),
		Host:   net.JoinHostPort(dbConfig.Host, dbConfig.Port),
		Path:   "/" + dbConfig.Name,
	}
	query := url.Values{}
	query.Set("sslmode", dbConfig.SSLMode)
	lockTimeout := dbConfig.LockTimeout
	if lockTimeout == 0 {
		lockTimeout = time.Second
	}
	statementTimeout := dbConfig.StatementTimeout
	if statementTimeout == 0 {
		statementTimeout = 3 * time.Second
	}
	query.Set("connect_timeout", "2")
	query.Set("lock_timeout", strconv.FormatInt(max(1, lockTimeout.Milliseconds()), 10))
	query.Set("statement_timeout", strconv.FormatInt(max(1, statementTimeout.Milliseconds()), 10))
	if dbConfig.SSLRootCert != "" {
		query.Set("sslrootcert", dbConfig.SSLRootCert)
	}
	connectionURL.RawQuery = query.Encode()
	return connectionURL.String()
}

func configurePool(db *sql.DB, dbConfig *config.DatabaseConfig) {
	maxOpen := dbConfig.MaxOpenConns
	if maxOpen == 0 {
		maxOpen = 20
	}
	maxIdle := dbConfig.MaxIdleConns
	if maxIdle == 0 {
		maxIdle = 5
	}
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(min(maxIdle, maxOpen))
	db.SetConnMaxIdleTime(time.Minute)
	db.SetConnMaxLifetime(30 * time.Minute)
}

func runMigrations(db *sql.DB, appEnvironment string) error {
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return fmt.Errorf("configure migration lock: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, os.DirFS("migrations"), goose.WithSessionLocker(locker))
	if err != nil {
		return fmt.Errorf("configure migration provider: %w", err)
	}
	if _, err := provider.Up(context.Background()); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	if appEnvironment == "demo" {
		demoProvider, err := goose.NewProvider(goose.DialectPostgres, db, os.DirFS("migrations/demo"),
			goose.WithSessionLocker(locker),
			goose.WithTableName("demo_goose_db_version"),
		)
		if err != nil {
			return fmt.Errorf("configure demo migration provider: %w", err)
		}
		if _, err := demoProvider.Up(context.Background()); err != nil {
			return fmt.Errorf("apply demo migrations: %w", err)
		}
	}
	return nil
}

// RequireNonDemoAdmin prevents production from starting with the documented
// demo password that is seeded for local demonstrations.
func RequireNonDemoAdmin(ctx context.Context, db *sql.DB) error {
	const demoPasswordHash = "$2a$10$6U17cK7/nLQco3NM6hxcguPsgIGwOUcABqK5KcAflYBZ9M/eN4IOm"
	var demoCredentialExists bool
	err := db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM admins WHERE password_hash = $1)`,
		demoPasswordHash,
	).Scan(&demoCredentialExists)
	if err != nil {
		return fmt.Errorf("check production admin credentials: %w", err)
	}
	if demoCredentialExists {
		return fmt.Errorf("production startup blocked: replace the seeded admin/admin123 credential or set APP_ENV=demo only for a demo deployment")
	}
	return nil
}
