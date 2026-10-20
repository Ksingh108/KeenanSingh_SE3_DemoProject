package config

import (
	"os"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	// Set required environment variables
	t.Setenv("DB_PASSWORD", "testpass")
	t.Setenv("APP_ENV", "demo")
	t.Setenv("DB_SSL_MODE", "")
	t.Setenv("DB_SSL_ROOT_CERT", "")

	config, err := Load()
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if config.Server.Port != "8080" {
		t.Errorf("Expected port 8080, got %s", config.Server.Port)
	}

	if config.Database.Password != "testpass" {
		t.Errorf("Expected password testpass, got %s", config.Database.Password)
	}
	if config.AppEnvironment != "demo" {
		t.Errorf("Expected app environment demo, got %s", config.AppEnvironment)
	}
	if config.Database.SSLMode != "verify-full" {
		t.Errorf("Demo SSL mode = %q, want verify-full", config.Database.SSLMode)
	}
	if config.Server.ReadTimeout != 15*time.Second || config.Server.WriteTimeout != 15*time.Second || config.Server.IdleTimeout != 60*time.Second {
		t.Errorf("server timeout defaults = (%s, %s, %s), want (15s, 15s, 60s)", config.Server.ReadTimeout, config.Server.WriteTimeout, config.Server.IdleTimeout)
	}
}

func TestLoad_RejectsInvalidServerTimeouts(t *testing.T) {
	t.Setenv("DB_PASSWORD", "testpass")
	t.Setenv("APP_ENV", "demo")

	for _, key := range []string{"READ_TIMEOUT", "WRITE_TIMEOUT", "IDLE_TIMEOUT"} {
		for _, value := range []string{"invalid", "0", "0s", "-1s", "9223372036854775807"} {
			t.Run(key+"="+value, func(t *testing.T) {
				t.Setenv(key, value)
				if _, err := Load(); err == nil {
					t.Fatalf("Load() accepted %s=%q", key, value)
				}
			})
		}
	}
}

func TestLoad_AcceptsIntegerTimeoutSeconds(t *testing.T) {
	t.Setenv("DB_PASSWORD", "testpass")
	t.Setenv("APP_ENV", "demo")
	t.Setenv("READ_TIMEOUT", "5")

	config, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if config.Server.ReadTimeout != 5*time.Second {
		t.Fatalf("ReadTimeout = %s, want 5s", config.Server.ReadTimeout)
	}
}

func TestLoad_DatabaseLimits(t *testing.T) {
	t.Setenv("DB_PASSWORD", "testpass")
	t.Setenv("APP_ENV", "demo")
	t.Setenv("DB_MAX_OPEN_CONNS", "12")
	t.Setenv("DB_MAX_IDLE_CONNS", "3")
	t.Setenv("DB_LOCK_TIMEOUT", "500ms")
	t.Setenv("DB_STATEMENT_TIMEOUT", "2s")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.MaxOpenConns != 12 || cfg.Database.MaxIdleConns != 3 || cfg.Database.LockTimeout != 500*time.Millisecond || cfg.Database.StatementTimeout != 2*time.Second {
		t.Fatalf("unexpected database limits: %+v", cfg.Database)
	}
	for _, key := range []string{"DB_MAX_OPEN_CONNS", "DB_MAX_IDLE_CONNS", "DB_LOCK_TIMEOUT", "DB_STATEMENT_TIMEOUT"} {
		for _, value := range []string{"invalid", "0", "-1"} {
			t.Run(key+"="+value, func(t *testing.T) {
				t.Setenv(key, value)
				if _, err := Load(); err == nil {
					t.Fatalf("accepted %s=%q", key, value)
				}
			})
		}
	}
	t.Setenv("DB_MAX_IDLE_CONNS", "13")
	if _, err := Load(); err == nil {
		t.Fatal("accepted idle limit greater than open limit")
	}
	t.Setenv("DB_MAX_IDLE_CONNS", "3")
	t.Setenv("DB_LOCK_TIMEOUT", "2s")
	if _, err := Load(); err == nil {
		t.Fatal("accepted lock timeout equal to statement timeout")
	}
}

func TestLoad_DefaultsToProduction(t *testing.T) {
	t.Setenv("DB_PASSWORD", "testpass")
	t.Setenv("APP_ENV", "")
	t.Setenv("DB_SSL_MODE", "")

	config, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if config.AppEnvironment != "production" {
		t.Fatalf("AppEnvironment = %q, want production", config.AppEnvironment)
	}
	if config.Database.SSLMode != "verify-full" {
		t.Fatalf("Database.SSLMode = %q, want verify-full", config.Database.SSLMode)
	}
}

func TestValidate_RejectsUnknownAppEnvironment(t *testing.T) {
	config := &Config{
		AppEnvironment: "staging",
		Server:         ServerConfig{Port: "8080"},
		Database:       DatabaseConfig{Host: "localhost", Password: "pass"},
		Security:       SecurityConfig{AllowedOrigins: []string{"http://localhost"}},
	}
	if err := config.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want invalid APP_ENV error")
	}
}

func TestLoad_MissingDBPassword(t *testing.T) {
	// Ensure DB_PASSWORD is not set
	if err := os.Unsetenv("DB_PASSWORD"); err != nil {
		t.Fatal(err)
	}

	_, err := Load()
	if err == nil {
		t.Error("Expected error when DB_PASSWORD is missing, got nil")
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name   string
		config *Config
		hasErr bool
	}{
		{
			name: "valid config",
			config: &Config{
				AppEnvironment: "production",
				Server:         ServerConfig{Port: "8080"},
				Database:       DatabaseConfig{Host: "localhost", Password: "pass", SSLMode: "verify-full"},
				Security:       SecurityConfig{AllowedOrigins: []string{"http://localhost"}},
			},
			hasErr: false,
		},
		{
			name: "missing port",
			config: &Config{
				Database: DatabaseConfig{Host: "localhost", Password: "pass"},
				Security: SecurityConfig{AllowedOrigins: []string{"http://localhost"}},
			},
			hasErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.hasErr {
				t.Errorf("Validate() error = %v, hasErr %v", err, tt.hasErr)
			}
		})
	}
}

func TestValidate_ProductionRequiresVerifiedTLS(t *testing.T) {
	for _, mode := range []string{"disable", "allow", "prefer", "require", "verify-ca"} {
		t.Run(mode, func(t *testing.T) {
			config := &Config{
				AppEnvironment: "production",
				Server:         ServerConfig{Port: "8080"},
				Database:       DatabaseConfig{Host: "db.example", Password: "pass", SSLMode: mode},
				Security:       SecurityConfig{AllowedOrigins: []string{"https://example.com"}},
			}
			if err := config.Validate(); err == nil {
				t.Fatalf("Validate() accepted DB_SSL_MODE=%q in production", mode)
			}
		})
	}
}

func TestValidate_DemoAllowsExplicitPlaintextOverride(t *testing.T) {
	config := &Config{
		AppEnvironment: "demo",
		Server:         ServerConfig{Port: "8080"},
		Database:       DatabaseConfig{Host: "localhost", Password: "pass", SSLMode: "disable"},
		Security:       SecurityConfig{AllowedOrigins: []string{"http://localhost:8080"}},
	}
	if err := config.Validate(); err != nil {
		t.Fatalf("Validate() rejected local demo configuration: %v", err)
	}
}
