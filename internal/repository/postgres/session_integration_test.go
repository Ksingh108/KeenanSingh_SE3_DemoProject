//go:build integration

package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/GringottsBankBranchBooking/internal/domain"
	"github.com/GringottsBankBranchBooking/internal/service"
)

func TestSessionRepository_CreateGetDelete(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	repo := NewSessionRepository(testDB)

	session := &domain.AdminSession{
		TokenHash: "hash-1",
		AdminID:   seededAdminID(t),
		ExpiresAt: utcNow().Add(time.Hour),
	}
	if err := repo.Create(ctx, session); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	got, err := repo.GetByTokenHash(ctx, "hash-1")
	if err != nil {
		t.Fatalf("GetByTokenHash failed: %v", err)
	}
	if got.AdminID != session.AdminID {
		t.Errorf("Expected admin %s, got %s", session.AdminID, got.AdminID)
	}

	if err := repo.Delete(ctx, "hash-1"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if _, err := repo.GetByTokenHash(ctx, "hash-1"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("Expected sql.ErrNoRows after delete, got %v", err)
	}
}

func TestSessionRepository_ExpiredSessionsAreInvisibleAndCleanedUp(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	repo := NewSessionRepository(testDB)
	adminID := seededAdminID(t)

	expired := &domain.AdminSession{TokenHash: "expired", AdminID: adminID, ExpiresAt: utcNow().Add(-time.Hour)}
	valid := &domain.AdminSession{TokenHash: "valid", AdminID: adminID, ExpiresAt: utcNow().Add(time.Hour)}
	for _, session := range []*domain.AdminSession{expired, valid} {
		if err := repo.Create(ctx, session); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	if _, err := repo.GetByTokenHash(ctx, "expired"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("Expected an expired session to be invisible, got %v", err)
	}
	if _, err := repo.GetByTokenHash(ctx, "valid"); err != nil {
		t.Errorf("Expected the valid session to be found, got %v", err)
	}

	if err := repo.CleanupExpired(ctx); err != nil {
		t.Fatalf("CleanupExpired failed: %v", err)
	}
	var rows int
	if err := testDB.QueryRow(`SELECT COUNT(*) FROM admin_sessions`).Scan(&rows); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if rows != 1 {
		t.Errorf("Expected only the valid session to remain, found %d rows", rows)
	}
}

// End to end against the real schema: after a login, the sessions table must
// hold the token's SHA-256 digest and never the token itself.
func TestAuthService_Login_PersistsOnlyTokenHash(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	authService := service.NewAuthService(NewAdminRepository(testDB), NewSessionRepository(testDB))

	token, err := authService.Login(ctx, seededAdminName, "admin123")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	var rawTokenRows int
	if err := testDB.QueryRow(`SELECT COUNT(*) FROM admin_sessions WHERE token_hash = $1`, token).Scan(&rawTokenRows); err != nil {
		t.Fatalf("query sessions: %v", err)
	}
	if rawTokenRows != 0 {
		t.Error("The raw session token must not be stored in admin_sessions")
	}

	sum := sha256.Sum256([]byte(token))
	var hashRows int
	if err := testDB.QueryRow(`SELECT COUNT(*) FROM admin_sessions WHERE token_hash = $1`, hex.EncodeToString(sum[:])).Scan(&hashRows); err != nil {
		t.Fatalf("query sessions: %v", err)
	}
	if hashRows != 1 {
		t.Errorf("Expected one session stored under the token's SHA-256 digest, found %d", hashRows)
	}

	adminID, valid := authService.ValidateToken(ctx, token)
	if !valid || adminID != seededAdminID(t) {
		t.Errorf("Expected the token to validate as the seeded admin, got %q (valid=%v)", adminID, valid)
	}
	// What a database leak would expose must not work as a session.
	if _, valid := authService.ValidateToken(ctx, hex.EncodeToString(sum[:])); valid {
		t.Error("The stored digest must not be accepted as a session token")
	}
}

func TestAdminRepository_GetByUsername(t *testing.T) {
	repo := NewAdminRepository(testDB)

	admin, err := repo.GetByUsername(context.Background(), seededAdminName)
	if err != nil {
		t.Fatalf("GetByUsername failed: %v", err)
	}
	if admin.Username != seededAdminName || admin.PasswordHash == "" {
		t.Errorf("Unexpected admin: %+v", admin)
	}

	if _, err := repo.GetByUsername(context.Background(), "nobody"); err == nil {
		t.Error("Expected an error for an unknown username")
	}
}
