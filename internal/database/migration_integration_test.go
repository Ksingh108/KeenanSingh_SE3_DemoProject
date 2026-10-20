//go:build integration

package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/go-connections/nat"
	"github.com/go-chi/chi/v5"
	"github.com/gringottsbankbranchbooking/internal/config"
	"github.com/gringottsbankbranchbooking/internal/handler"
	"github.com/gringottsbankbranchbooking/internal/health"
	"github.com/gringottsbankbranchbooking/internal/middleware"
	"github.com/gringottsbankbranchbooking/internal/repository/postgres"
	"github.com/gringottsbankbranchbooking/internal/service"
	"github.com/lib/pq"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestDatabaseFailureUnderLoad(t *testing.T) {
	t.Chdir(filepath.Join("..", ".."))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	// The test restarts the container and keeps using one connection pool, so the
	// host port must survive the restart; Docker otherwise assigns a new random one.
	hostPort := freeTCPPort(t)
	container, err := tcpostgres.Run(ctx, "postgres:15-alpine",
		tcpostgres.WithDatabase("gringotts_test"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		testcontainers.WithHostConfigModifier(func(hostConfig *container.HostConfig) {
			hostConfig.PortBindings = nat.PortMap{"5432/tcp": {{HostPort: hostPort}}}
		}),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	defer func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Errorf("terminate postgres: %v", err)
		}
	}()
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	connectionURL, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	settings := &config.DatabaseConfig{
		Host: connectionURL.Hostname(), Port: connectionURL.Port(),
		User: "test", Password: "test", Name: "gringotts_test", SSLMode: "disable",
		MaxOpenConns: 4, MaxIdleConns: 2,
		LockTimeout: 100 * time.Millisecond, StatementTimeout: time.Second,
	}
	db, err := sql.Open("postgres", buildConnectionString(settings))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})
	configurePool(db, settings)
	if err := runMigrations(db, "demo"); err != nil {
		t.Fatal(err)
	}

	bookingHandler := handler.NewBookingHandler(service.NewBookingService(
		postgres.NewBookingRepository(db), postgres.NewBranchRepository(db)), nil)
	checker := health.NewHealthChecker(db)
	router := chi.NewRouter()
	router.Use(middleware.RequestDeadline(5 * time.Second))
	router.Post("/api/bookings", bookingHandler.CreateBooking)
	router.Get("/ready", checker.ReadinessHandler)
	router.Get("/live", health.LivenessHandler)
	date := time.Now().AddDate(0, 0, 2).Format("2006-01-02")
	body := fmt.Sprintf(`{"branch_id":"550e8400-e29b-41d4-a716-446655440001","service_id":"660e8400-e29b-41d4-a716-446655440001","customer_name":"Jane Smith","email":"jane@example.com","phone":"+27 21 555 0100","date":%q,"time_slot":"09:00"}`, date)
	serve := func(method, path, remote string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.RemoteAddr = remote
		request.Header.Set("Content-Type", "application/json")
		result := httptest.NewRecorder()
		router.ServeHTTP(result, request)
		return result
	}
	if result := serve(http.MethodGet, "/ready", ""); result.Code != http.StatusOK {
		t.Fatalf("initial readiness = %d: %s", result.Code, result.Body)
	}
	runFailedBookings := func(t *testing.T) {
		t.Helper()
		start := time.Now()
		var done sync.WaitGroup
		for index := 0; index < 40; index++ {
			done.Add(1)
			go func() {
				defer done.Done()
				result := serve(http.MethodPost, "/api/bookings", fmt.Sprintf("192.0.2.%d:1234", index+1))
				if result.Code != http.StatusServiceUnavailable || result.Header().Get("Retry-After") != "1" {
					t.Errorf("booking response = %d, headers = %v, body = %s", result.Code, result.Header(), result.Body)
				}
			}()
		}
		done.Wait()
		if elapsed := time.Since(start); elapsed > 7*time.Second {
			t.Errorf("failed bookings took %s, want at most 7s", elapsed)
		}
		if stats := db.Stats(); stats.MaxOpenConnections != 4 || stats.OpenConnections > 4 {
			t.Errorf("pool exceeded connection budget: %+v", stats)
		}
	}

	t.Run("lock timeout", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, err := tx.ExecContext(ctx, `SELECT id FROM branches WHERE id = '550e8400-e29b-41d4-a716-446655440001' FOR UPDATE`); err != nil {
			t.Fatal(err)
		}
		_, err = db.ExecContext(ctx, `SELECT id FROM branches WHERE id = '550e8400-e29b-41d4-a716-446655440001' FOR UPDATE`)
		var pgError *pq.Error
		if !errors.As(err, &pgError) || pgError.Code != "55P03" {
			t.Fatalf("lock timeout error = %v, want SQLSTATE 55P03", err)
		}
		runFailedBookings(t)
	})
	t.Run("statement timeout", func(t *testing.T) {
		start := time.Now()
		_, err := db.ExecContext(ctx, "SELECT pg_sleep(10)")
		var pgError *pq.Error
		if !errors.As(err, &pgError) || pgError.Code != "57014" {
			t.Fatalf("statement timeout error = %v, want SQLSTATE 57014", err)
		}
		if time.Since(start) > 3*time.Second {
			t.Fatal("statement did not fail promptly")
		}
	})
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM bookings").Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed requests persisted bookings: count = %d, error = %v", count, err)
	}
	if err := container.Stop(ctx, nil); err != nil {
		t.Fatalf("stop postgres: %v", err)
	}
	runFailedBookings(t)
	if result := serve(http.MethodGet, "/ready", ""); result.Code != http.StatusServiceUnavailable || result.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("outage readiness = %d, headers = %v", result.Code, result.Header())
	}
	if result := serve(http.MethodGet, "/live", ""); result.Code != http.StatusOK {
		t.Fatalf("outage liveness = %d", result.Code)
	}
	if err := container.Start(ctx); err != nil {
		t.Fatalf("restart postgres: %v", err)
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	recovery, recoveryCancel := context.WithTimeout(ctx, 20*time.Second)
	defer recoveryCancel()
	for {
		ready := serve(http.MethodGet, "/ready", "")
		if ready.Code == http.StatusOK {
			break
		}
		select {
		case <-recovery.Done():
			// Docker may publish a different host port after a restart; the pool
			// keeps dialling the original one, so report both.
			restartedPort, portErr := container.MappedPort(ctx, "5432/tcp")
			t.Fatalf("readiness did not recover: pool dials port %s, container now maps %s (lookup error: %v); last response %d %s",
				settings.Port, restartedPort.Port(), portErr, ready.Code, ready.Body)
		case <-ticker.C:
		}
	}
	if result := serve(http.MethodPost, "/api/bookings", "203.0.113.200:1234"); result.Code != http.StatusCreated {
		t.Fatalf("booking after recovery = %d: %s", result.Code, result.Body)
	}
}

func TestFreshSchemaMigrationsEnforceConstraints(t *testing.T) {
	t.Chdir(filepath.Join("..", ".."))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	container, err := tcpostgres.Run(ctx, "postgres:15-alpine",
		tcpostgres.WithDatabase("gringotts_test"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start postgres container (is Docker running?): %v", err)
	}
	defer func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Errorf("terminate postgres container: %v", err)
		}
	}()

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("get database connection string: %v", err)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping database: %v", err)
	}

	if err := runMigrations(db, "production"); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	var timezoneAwareColumns int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND data_type = 'timestamp with time zone'
		  AND (table_name, column_name) IN (
			  ('branches', 'created_at'),
			  ('services', 'created_at'),
			  ('bookings', 'created_at'),
			  ('admins', 'created_at'),
			  ('admin_sessions', 'expires_at'),
			  ('admin_sessions', 'created_at'),
			  ('admin_audit_log', 'created_at')
		  )
	`).Scan(&timezoneAwareColumns); err != nil {
		t.Fatalf("check timestamp column types: %v", err)
	}
	if timezoneAwareColumns != 7 {
		t.Fatalf("expected all 7 persisted time columns to use TIMESTAMPTZ, got %d", timezoneAwareColumns)
	}
	if err := RequireNonDemoAdmin(ctx, db); err != nil {
		t.Fatalf("production schema should not contain a demo admin: %v", err)
	}
	if err := runMigrations(db, "demo"); err != nil {
		t.Fatalf("apply demo migrations: %v", err)
	}
	if err := RequireNonDemoAdmin(ctx, db); err == nil {
		t.Fatal("production credential check should reject the seeded demo admin")
	}
	if _, err := db.ExecContext(ctx, `UPDATE admins SET password_hash = 'replaced-demo-hash' WHERE username = 'admin'`); err != nil {
		t.Fatalf("replace seeded admin password hash: %v", err)
	}
	if err := RequireNonDemoAdmin(ctx, db); err != nil {
		t.Fatalf("production credential check should allow a replaced admin password: %v", err)
	}

	if _, err := db.ExecContext(ctx, `UPDATE branches SET capacity = 0 WHERE id = '550e8400-e29b-41d4-a716-446655440002'`); err == nil {
		t.Fatal("expected database to reject non-positive branch capacity")
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO bookings (id, branch_id, service_id, customer_name, email, phone, date, time_slot)
		VALUES ('770e8400-e29b-41d4-a716-446655440001',
		        '550e8400-e29b-41d4-a716-446655440001',
		        '660e8400-e29b-41d4-a716-446655440001',
		        'Valid Customer', 'valid@example.com', '+27 21 555 0100', '2030-01-02', '09:00')
	`); err != nil {
		t.Fatalf("insert valid booking: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO bookings (branch_id, service_id, customer_name, email, phone, date, time_slot)
		VALUES ('550e8400-e29b-41d4-a716-446655440002',
		        '660e8400-e29b-41d4-a716-446655440001',
		        'Invalid Customer', 'invalid@example.com', '+27 21 555 0100', '2030-01-02', '09:00')
	`); err == nil {
		t.Fatal("expected database to reject a service not offered at the branch")
	}
	for _, check := range []struct {
		name  string
		query string
	}{
		{"booking phone", `UPDATE bookings SET phone = NULL WHERE id = '770e8400-e29b-41d4-a716-446655440001'`},
		{"booking creation time", `UPDATE bookings SET created_at = NULL WHERE id = '770e8400-e29b-41d4-a716-446655440001'`},
		{"service description", `UPDATE services SET description = NULL WHERE id = '660e8400-e29b-41d4-a716-446655440001'`},
		{"service creation time", `UPDATE services SET created_at = NULL WHERE id = '660e8400-e29b-41d4-a716-446655440001'`},
		{"branch creation time", `UPDATE branches SET created_at = NULL WHERE id = '550e8400-e29b-41d4-a716-446655440001'`},
	} {
		if _, err := db.ExecContext(ctx, check.query); err == nil {
			t.Errorf("expected database to reject null %s", check.name)
		}
	}
	if _, err := db.ExecContext(ctx, `UPDATE bookings SET status = 'pending' WHERE id = '770e8400-e29b-41d4-a716-446655440001'`); err == nil {
		t.Fatal("expected database to reject invalid booking status")
	}

	var auditID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO admin_audit_log (admin_id, action, target_type, target_id)
		SELECT id, 'test', 'booking', 'x' FROM admins LIMIT 1
		RETURNING id
	`).Scan(&auditID); err != nil {
		t.Fatalf("insert audit entry: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE admin_audit_log SET details = 'tampered' WHERE id = $1`, auditID); err == nil {
		t.Fatal("expected database to reject updates to the audit log")
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM admin_audit_log WHERE id = $1`, auditID); err == nil {
		t.Fatal("expected database to reject deletes from the audit log")
	}

	if err := runMigrations(db, "demo"); err != nil {
		t.Fatalf("second migration run should be a no-op: %v", err)
	}
}

// freeTCPPort returns a currently unused local TCP port as a string.
func freeTCPPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	defer func() {
		if err := listener.Close(); err != nil {
			t.Errorf("close port probe: %v", err)
		}
	}()
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	return port
}
