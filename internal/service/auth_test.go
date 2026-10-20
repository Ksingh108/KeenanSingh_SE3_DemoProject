package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/gringottsbankbranchbooking/internal/domain"
	"github.com/gringottsbankbranchbooking/internal/repository/memory"
	"golang.org/x/crypto/bcrypt"
)

func TestAuthService_Login_Success(t *testing.T) {
	adminRepo := memory.NewAdminRepository()
	sessionRepo := memory.NewSessionRepository()
	service := NewAuthService(adminRepo, sessionRepo)

	// Create test admin
	hashedPassword, _ := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.DefaultCost)
	admin := &domain.Admin{
		ID:           "admin1",
		Username:     "testadmin",
		PasswordHash: string(hashedPassword),
	}
	if err := adminRepo.Create(admin); err != nil {
		t.Fatalf("Create admin failed: %v", err)
	}

	token, err := service.Login(context.Background(), "testadmin", "password")

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if token == "" {
		t.Error("Expected token, got empty string")
	}
}

func TestAuthService_Login_InvalidCredentials(t *testing.T) {
	adminRepo := memory.NewAdminRepository()
	sessionRepo := memory.NewSessionRepository()
	service := NewAuthService(adminRepo, sessionRepo)

	_, err := service.Login(context.Background(), "invalid", "invalid")

	if err == nil {
		t.Error("Expected error for invalid credentials")
	}
}

func TestAuthService_Login_TokenGenerationFailure(t *testing.T) {
	adminRepo := memory.NewAdminRepository()
	sessionRepo := memory.NewSessionRepository()
	service := NewAuthService(adminRepo, sessionRepo)

	hashedPassword, _ := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.DefaultCost)
	admin := &domain.Admin{
		ID:           "admin1",
		Username:     "testadmin",
		PasswordHash: string(hashedPassword),
	}
	if err := adminRepo.Create(admin); err != nil {
		t.Fatalf("Create admin failed: %v", err)
	}

	oldReader := secureRandomReader
	secureRandomReader = failingReader{}
	defer func() { secureRandomReader = oldReader }()

	_, err := service.Login(context.Background(), "testadmin", "password")
	if err == nil {
		t.Error("Expected token generation error")
	}
}

type failingReader struct{}

func (f failingReader) Read(p []byte) (int, error) {
	return 0, errors.New("crypto rand failed")
}

func TestAuthService_ValidateToken_Valid(t *testing.T) {
	adminRepo := memory.NewAdminRepository()
	sessionRepo := memory.NewSessionRepository()
	service := NewAuthService(adminRepo, sessionRepo)

	// Create valid session
	token := "valid-token"
	session := &domain.AdminSession{
		TokenHash: hashToken(token),
		AdminID:   "admin1",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := sessionRepo.Create(context.Background(), session); err != nil {
		t.Fatalf("Create session failed: %v", err)
	}

	_, valid := service.ValidateToken(context.Background(), token)

	if !valid {
		t.Error("Expected token to be valid")
	}
}

func TestAuthService_ValidateToken_Expired(t *testing.T) {
	adminRepo := memory.NewAdminRepository()
	sessionRepo := memory.NewSessionRepository()
	service := NewAuthService(adminRepo, sessionRepo)

	// Create expired session
	token := "expired-token"
	session := &domain.AdminSession{
		TokenHash: hashToken(token),
		AdminID:   "admin1",
		ExpiresAt: time.Now().Add(-time.Hour),
	}
	if err := sessionRepo.Create(context.Background(), session); err != nil {
		t.Fatalf("Create session failed: %v", err)
	}

	_, valid := service.ValidateToken(context.Background(), token)

	if valid {
		t.Error("Expected expired token to be invalid")
	}
}

// newLoggedInSession logs in a test admin and returns the raw token together
// with the session repository so tests can inspect what was persisted.
func newLoggedInSession(t *testing.T) (*AuthService, *memory.SessionRepository, string) {
	t.Helper()

	adminRepo := memory.NewAdminRepository()
	sessionRepo := memory.NewSessionRepository()
	service := NewAuthService(adminRepo, sessionRepo)

	hashedPassword, _ := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.DefaultCost)
	if err := adminRepo.Create(&domain.Admin{
		ID:           "admin1",
		Username:     "testadmin",
		PasswordHash: string(hashedPassword),
	}); err != nil {
		t.Fatalf("Create admin failed: %v", err)
	}

	token, err := service.Login(context.Background(), "testadmin", "password")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	return service, sessionRepo, token
}

func TestAuthService_Login_StoresOnlyTokenHash(t *testing.T) {
	_, sessionRepo, token := newLoggedInSession(t)
	ctx := context.Background()

	// The raw token must not be usable as a lookup key in the store.
	if _, err := sessionRepo.GetByTokenHash(ctx, token); err == nil {
		t.Error("Raw token must not be stored; found a session keyed by it")
	}

	// The stored key is the SHA-256 digest, computed independently here.
	sum := sha256.Sum256([]byte(token))
	expectedHash := hex.EncodeToString(sum[:])

	session, err := sessionRepo.GetByTokenHash(ctx, expectedHash)
	if err != nil {
		t.Fatalf("Expected session stored under the token's SHA-256 digest: %v", err)
	}
	if session.TokenHash == token {
		t.Error("Stored TokenHash must differ from the raw token")
	}
	if len(session.TokenHash) != 64 {
		t.Errorf("Expected 64-char hex digest, got %d chars", len(session.TokenHash))
	}
}

// A leaked sessions table must not hand out live sessions: presenting the
// stored digest as if it were the cookie token has to fail.
func TestAuthService_ValidateToken_RejectsStoredHash(t *testing.T) {
	service, _, token := newLoggedInSession(t)
	ctx := context.Background()

	if _, valid := service.ValidateToken(ctx, token); !valid {
		t.Fatal("Expected the issued token to be valid")
	}
	if _, valid := service.ValidateToken(ctx, hashToken(token)); valid {
		t.Error("The stored digest must not be accepted as a session token")
	}
}

func TestAuthService_ValidateToken_ReturnsAdminID(t *testing.T) {
	service, _, token := newLoggedInSession(t)

	adminID, valid := service.ValidateToken(context.Background(), token)

	if !valid {
		t.Fatal("Expected the issued token to be valid")
	}
	if adminID != "admin1" {
		t.Errorf("Expected admin ID %q, got %q", "admin1", adminID)
	}
}

func TestAuthService_ValidateToken_InvalidReturnsNoAdminID(t *testing.T) {
	service, _, _ := newLoggedInSession(t)

	adminID, valid := service.ValidateToken(context.Background(), "not-a-real-token")

	if valid {
		t.Error("Expected unknown token to be invalid")
	}
	if adminID != "" {
		t.Errorf("Expected no admin ID for an invalid token, got %q", adminID)
	}
}

func TestAuthService_Logout_InvalidatesToken(t *testing.T) {
	service, _, token := newLoggedInSession(t)
	ctx := context.Background()

	if err := service.Logout(ctx, token); err != nil {
		t.Fatalf("Logout failed: %v", err)
	}
	if _, valid := service.ValidateToken(ctx, token); valid {
		t.Error("Expected token to be invalid after logout")
	}
}

func TestHashToken_Deterministic(t *testing.T) {
	sum := sha256.Sum256([]byte("abc"))
	want := hex.EncodeToString(sum[:])
	if got := hashToken("abc"); got != want {
		t.Error("Expected the same token to produce the same digest")
	}
	if hashToken("abc") == hashToken("abd") {
		t.Error("Expected different tokens to produce different digests")
	}
}

type failingAdminRepo struct{}

func (failingAdminRepo) GetByUsername(context.Context, string) (*domain.Admin, error) {
	return nil, errors.New("connection refused")
}

func TestAuthService_Login_ErrorClassification(t *testing.T) {
	sessions := memory.NewSessionRepository()

	_, err := NewAuthService(memory.NewAdminRepository(), sessions).Login(context.Background(), "nobody", "x")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("unknown user: got %v, want ErrInvalidCredentials", err)
	}

	_, err = NewAuthService(memory.NewAdminRepository(), sessions).Login(context.Background(), "admin", "wrong")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("wrong password: got %v, want ErrInvalidCredentials", err)
	}

	_, err = NewAuthService(failingAdminRepo{}, sessions).Login(context.Background(), "admin", "x")
	if err == nil || errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("backend failure: got %v, want a non-credentials error", err)
	}
}
