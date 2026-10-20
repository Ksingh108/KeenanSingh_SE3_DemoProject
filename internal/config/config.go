package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	AppEnvironment string         `json:"app_environment"`
	Server         ServerConfig   `json:"server"`
	Database       DatabaseConfig `json:"database"`
	Security       SecurityConfig `json:"security"`
}

type ServerConfig struct {
	Port         string        `json:"port"`
	ReadTimeout  time.Duration `json:"read_timeout"`
	WriteTimeout time.Duration `json:"write_timeout"`
	IdleTimeout  time.Duration `json:"idle_timeout"`
}

type DatabaseConfig struct {
	Host             string        `json:"host"`
	Port             string        `json:"port"`
	User             string        `json:"user"`
	Password         string        `json:"password"`
	Name             string        `json:"name"`
	SSLMode          string        `json:"ssl_mode"`
	SSLRootCert      string        `json:"ssl_root_cert"`
	MaxOpenConns     int           `json:"max_open_conns"`
	MaxIdleConns     int           `json:"max_idle_conns"`
	LockTimeout      time.Duration `json:"lock_timeout"`
	StatementTimeout time.Duration `json:"statement_timeout"`
}

type SecurityConfig struct {
	AllowedOrigins []string `json:"allowed_origins"`
}

func Load() (*Config, error) {
	password, err := getEnvRequired("DB_PASSWORD")
	if err != nil {
		return nil, err
	}
	appEnvironment := getEnv("APP_ENV", "production")
	defaultSSLMode := "verify-full"
	readTimeout, err := getDurationEnv("READ_TIMEOUT", 15*time.Second)
	if err != nil {
		return nil, err
	}
	writeTimeout, err := getDurationEnv("WRITE_TIMEOUT", 15*time.Second)
	if err != nil {
		return nil, err
	}
	idleTimeout, err := getDurationEnv("IDLE_TIMEOUT", 60*time.Second)
	if err != nil {
		return nil, err
	}
	maxOpenConns, err := getPositiveIntEnv("DB_MAX_OPEN_CONNS", 20)
	if err != nil {
		return nil, err
	}
	maxIdleConns, err := getPositiveIntEnv("DB_MAX_IDLE_CONNS", 5)
	if err != nil {
		return nil, err
	}
	lockTimeout, err := getDurationEnv("DB_LOCK_TIMEOUT", time.Second)
	if err != nil {
		return nil, err
	}
	statementTimeout, err := getDurationEnv("DB_STATEMENT_TIMEOUT", 3*time.Second)
	if err != nil {
		return nil, err
	}

	config := &Config{
		AppEnvironment: appEnvironment,
		Server: ServerConfig{
			Port:         getEnv("PORT", "8080"),
			ReadTimeout:  readTimeout,
			WriteTimeout: writeTimeout,
			IdleTimeout:  idleTimeout,
		},
		Database: DatabaseConfig{
			Host:             getEnv("DB_HOST", "localhost"),
			Port:             getEnv("DB_PORT", "5432"),
			User:             getEnv("DB_USER", "gringotts"),
			Password:         password,
			Name:             getEnv("DB_NAME", "bookings"),
			SSLMode:          getEnv("DB_SSL_MODE", defaultSSLMode),
			SSLRootCert:      getEnv("DB_SSL_ROOT_CERT", ""),
			MaxOpenConns:     maxOpenConns,
			MaxIdleConns:     maxIdleConns,
			LockTimeout:      lockTimeout,
			StatementTimeout: statementTimeout,
		},
		Security: SecurityConfig{
			AllowedOrigins: getSliceEnv("ALLOWED_ORIGINS", []string{"http://localhost:3000", "http://localhost:8080"}),
		},
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return config, nil
}

func (c *Config) Validate() error {
	if c.Server.Port == "" {
		return fmt.Errorf("server port is required")
	}
	if c.AppEnvironment != "demo" && c.AppEnvironment != "production" {
		return fmt.Errorf("APP_ENV must be either demo or production")
	}
	if c.Database.Host == "" {
		return fmt.Errorf("database host is required")
	}
	if c.Database.MaxOpenConns < 0 || c.Database.MaxIdleConns < 0 {
		return fmt.Errorf("database pool limits must not be negative")
	}
	if c.Database.MaxOpenConns > 0 && c.Database.MaxIdleConns > c.Database.MaxOpenConns {
		return fmt.Errorf("DB_MAX_IDLE_CONNS must not exceed DB_MAX_OPEN_CONNS")
	}
	if c.Database.LockTimeout < 0 || c.Database.StatementTimeout < 0 {
		return fmt.Errorf("database timeouts must not be negative")
	}
	if c.Database.StatementTimeout > 0 && c.Database.LockTimeout >= c.Database.StatementTimeout {
		return fmt.Errorf("DB_LOCK_TIMEOUT must be shorter than DB_STATEMENT_TIMEOUT")
	}
	switch c.Database.SSLMode {
	case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
	default:
		return fmt.Errorf("DB_SSL_MODE must be a valid PostgreSQL SSL mode")
	}
	if c.AppEnvironment == "production" && c.Database.SSLMode != "verify-full" {
		return fmt.Errorf("DB_SSL_MODE must be verify-full in production")
	}
	if len(c.Security.AllowedOrigins) == 0 {
		return fmt.Errorf("at least one allowed origin is required")
	}
	return nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getPositiveIntEnv(key string, defaultValue int) (int, error) {
	value := getEnv(key, strconv.Itoa(defaultValue))
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return parsed, nil
}

func getDurationEnv(key string, defaultValue time.Duration) (time.Duration, error) {
	if value := os.Getenv(key); value != "" {
		duration, err := time.ParseDuration(value)
		if err != nil {
			seconds, parseErr := strconv.ParseInt(value, 10, 64)
			if parseErr != nil {
				return 0, fmt.Errorf("%s must be a valid duration or integer seconds: %w", key, err)
			}
			duration, err = time.ParseDuration(strconv.FormatInt(seconds, 10) + "s")
			if err != nil {
				return 0, fmt.Errorf("%s is out of range: %w", key, err)
			}
		}
		if duration <= 0 {
			return 0, fmt.Errorf("%s must be greater than zero", key)
		}
		return duration, nil
	}
	return defaultValue, nil
}

func getSliceEnv(key string, defaultValue []string) []string {
	if value := os.Getenv(key); value != "" {
		return strings.Split(value, ",")
	}
	return defaultValue
}

func getEnvRequired(key string) (string, error) {
	value := os.Getenv(key)
	if value == "" {
		return "", fmt.Errorf("%s environment variable is required", key)
	}
	return value, nil
}
