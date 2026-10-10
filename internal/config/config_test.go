package config

import (
	"os"
	"testing"
)

func TestLoad(t *testing.T) {
	// Set required environment variables
	os.Setenv("DB_PASSWORD", "testpass")
	defer os.Unsetenv("DB_PASSWORD")

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
}

func TestLoad_MissingDBPassword(t *testing.T) {
	// Ensure DB_PASSWORD is not set
	os.Unsetenv("DB_PASSWORD")

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
				Server:   ServerConfig{Port: "8080"},
				Database: DatabaseConfig{Host: "localhost", Password: "pass"},
				Security: SecurityConfig{AllowedOrigins: []string{"http://localhost"}},
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
