package memory

import (
	"context"
	"time"

	"github.com/gringottsbankbranchbooking/internal/domain"
	"golang.org/x/crypto/bcrypt"
)

type AdminRepository struct {
	admins map[string]*domain.Admin
}

func NewAdminRepository() *AdminRepository {
	repo := &AdminRepository{
		admins: make(map[string]*domain.Admin),
	}
	repo.seedData()
	return repo
}

func (r *AdminRepository) seedData() {
	hashedPassword, _ := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.DefaultCost)
	r.admins["admin"] = &domain.Admin{
		ID:           "admin1",
		Username:     "admin",
		PasswordHash: string(hashedPassword),
		CreatedAt:    time.Now(),
	}
}

func (r *AdminRepository) GetByUsername(ctx context.Context, username string) (*domain.Admin, error) {
	admin, exists := r.admins[username]
	if !exists {
		return nil, domain.ErrAdminNotFound
	}
	return admin, nil
}

func (r *AdminRepository) Create(admin *domain.Admin) error {
	r.admins[admin.Username] = admin
	return nil
}
