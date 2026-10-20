package database

import (
	"database/sql"
	"net/url"
	"testing"
	"time"

	"github.com/gringottsbankbranchbooking/internal/config"
)

func TestBuildConnectionString(t *testing.T) {
	dbConfig := &config.DatabaseConfig{
		Host:        "db.example.com",
		Port:        "5432",
		User:        "app user",
		Password:    "p@ss/word",
		Name:        "booking db",
		SSLMode:     "verify-full",
		SSLRootCert: "/etc/ssl/db ca.crt",
	}

	connectionURL, err := url.Parse(buildConnectionString(dbConfig))
	if err != nil {
		t.Fatalf("parse connection URL: %v", err)
	}
	username := connectionURL.User.Username()
	password, ok := connectionURL.User.Password()
	if !ok || username != dbConfig.User || password != dbConfig.Password {
		t.Errorf("connection credentials = (%q, %q), want configured values", username, password)
	}
	if connectionURL.Host != "db.example.com:5432" || connectionURL.Path != "/booking db" {
		t.Errorf("connection target = %q%s, want db.example.com:5432/booking db", connectionURL.Host, connectionURL.Path)
	}
	if query := connectionURL.Query(); query.Get("sslmode") != "verify-full" || query.Get("sslrootcert") != dbConfig.SSLRootCert {
		t.Errorf("TLS query parameters = %v, want verify-full and configured CA", query)
	}
	query := connectionURL.Query()
	if query.Get("connect_timeout") != "2" || query.Get("lock_timeout") != "1000" || query.Get("statement_timeout") != "3000" {
		t.Fatalf("timeout parameters = %v", query)
	}
	dbConfig.LockTimeout = 500 * time.Millisecond
	dbConfig.StatementTimeout = 2 * time.Second
	customURL, err := url.Parse(buildConnectionString(dbConfig))
	if err != nil {
		t.Fatal(err)
	}
	if customURL.Query().Get("lock_timeout") != "500" || customURL.Query().Get("statement_timeout") != "2000" {
		t.Fatalf("custom timeout parameters = %v", customURL.Query())
	}
}

func TestConfigurePool(t *testing.T) {
	for _, limit := range []int{0, 7} {
		db, err := sql.Open("postgres", "")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		configurePool(db, &config.DatabaseConfig{MaxOpenConns: limit})
		want := limit
		if want == 0 {
			want = 20
		}
		if got := db.Stats().MaxOpenConnections; got != want {
			t.Fatalf("MaxOpenConnections = %d, want %d", got, want)
		}
	}
}
