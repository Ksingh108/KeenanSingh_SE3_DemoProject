package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/GringottsBankBranchBooking/internal/config"
	_ "github.com/lib/pq"
	"log/slog"
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
func NewConnection(dbConfig *config.DatabaseConfig) (*sql.DB, error) {
	// Build connection string using provided config (no hardcoded defaults)
	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		dbConfig.Host, dbConfig.Port, dbConfig.User, dbConfig.Password, dbConfig.Name, dbConfig.SSLMode)

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, err
	}

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
