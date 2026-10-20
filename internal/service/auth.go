package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/gringottsbankbranchbooking/internal/domain"
	"github.com/gringottsbankbranchbooking/internal/repository"
	"golang.org/x/crypto/bcrypt"
)

var secureRandomReader io.Reader = rand.Reader

// ErrInvalidCredentials is returned for an unknown username or a wrong password.
// Any other Login error is an internal failure.
var ErrInvalidCredentials = errors.New("invalid credentials")

// dummyPasswordHash is compared against when the username is unknown so that
// unknown and known usernames cost the same bcrypt work.
var dummyPasswordHash, _ = bcrypt.GenerateFromPassword([]byte("dummy-password"), bcrypt.DefaultCost)

type AuthService struct {
	adminRepo   repository.AdminRepository
	sessionRepo repository.SessionRepository
}

func NewAuthService(adminRepo repository.AdminRepository, sessionRepo repository.SessionRepository) *AuthService {
	return &AuthService{
		adminRepo:   adminRepo,
		sessionRepo: sessionRepo,
	}
}

func (s *AuthService) Login(ctx context.Context, username, password string) (string, error) {
	admin, err := s.adminRepo.GetByUsername(ctx, username)
	if errors.Is(err, domain.ErrAdminNotFound) {
		_ = bcrypt.CompareHashAndPassword(dummyPasswordHash, []byte(password))
		return "", ErrInvalidCredentials
	}
	if err != nil {
		return "", fmt.Errorf("look up admin: %w", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(password)); err != nil {
		return "", ErrInvalidCredentials
	}

	token, err := s.generateToken()
	if err != nil {
		return "", fmt.Errorf("generate session token: %w", err)
	}

	// Only the digest is persisted; the raw token goes to the client's cookie
	// and nowhere else.
	session := &domain.AdminSession{
		TokenHash: hashToken(token),
		AdminID:   admin.ID,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}

	if err := s.sessionRepo.Create(ctx, session); err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}

	return token, nil
}

// ValidateToken checks a session token and, when it is valid, returns the ID
// of the admin it belongs to so callers can attribute their actions.
func (s *AuthService) ValidateToken(ctx context.Context, token string) (adminID string, valid bool) {
	session, err := s.sessionRepo.GetByTokenHash(ctx, hashToken(token))
	if err != nil {
		return "", false
	}
	if !time.Now().Before(session.ExpiresAt) {
		return "", false
	}
	return session.AdminID, true
}

func (s *AuthService) Logout(ctx context.Context, token string) error {
	return s.sessionRepo.Delete(ctx, hashToken(token))
}

// hashToken returns the hex-encoded SHA-256 digest of a session token. A fast
// hash is appropriate here, unlike for passwords: the token is 256 bits of
// CSPRNG output, so it cannot be brute-forced or looked up in a rainbow table,
// and the digest is computed on every authenticated request.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *AuthService) generateToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := io.ReadFull(secureRandomReader, bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
