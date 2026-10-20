package memory

import (
	"context"
	"sync"

	"github.com/google/uuid"

	"github.com/gringottsbankbranchbooking/internal/domain"
)

type BranchRepository struct {
	mu       sync.RWMutex
	branches map[string]*domain.Branch
	archived map[string]*domain.Branch
}

func NewBranchRepository() *BranchRepository {
	repo := &BranchRepository{
		branches: make(map[string]*domain.Branch),
		archived: make(map[string]*domain.Branch),
	}
	// In-memory repositories are test fixtures, not a database migration or
	// production bootstrap path. Real schema and startup data are managed by Goose;
	// this seed only gives unit tests a known set
	// of branches and services to work with.
	repo.seedData()
	return repo
}

func (r *BranchRepository) seedData() {
	branches := []*domain.Branch{
		{
			ID:       "550e8400-e29b-41d4-a716-446655440001",
			Name:     "Diagon Alley Main",
			Address:  "93 Diagon Alley",
			City:     "Johannesburg",
			Phone:    "+27 11 555 0901",
			Hours:    "Mon-Fri 9:00-17:00, Sat 9:00-13:00",
			Capacity: 15,
			Services: []*domain.Service{
				{ID: "660e8400-e29b-41d4-a716-446655440001", Name: "Account Opening"},
				{ID: "660e8400-e29b-41d4-a716-446655440002", Name: "Vault Access"},
				{ID: "660e8400-e29b-41d4-a716-446655440003", Name: "Currency Exchange"},
				{ID: "660e8400-e29b-41d4-a716-446655440004", Name: "Loan Consultation"},
			},
		},
		{
			ID:       "550e8400-e29b-41d4-a716-446655440002",
			Name:     "Knockturn Alley",
			Address:  "12 Knockturn Alley",
			City:     "Johannesburg",
			Phone:    "+27 11 555 0902",
			Hours:    "Mon-Fri 10:00-16:00",
			Capacity: 8,
			Services: []*domain.Service{
				{ID: "660e8400-e29b-41d4-a716-446655440002", Name: "Vault Access"},
				{ID: "660e8400-e29b-41d4-a716-446655440003", Name: "Currency Exchange"},
			},
		},
		{
			ID:       "550e8400-e29b-41d4-a716-446655440003",
			Name:     "Hogsmeade Village",
			Address:  "1 High Street",
			City:     "Cape Town",
			Phone:    "+27 21 555 0903",
			Hours:    "Mon-Sat 9:00-17:00",
			Capacity: 12,
			Services: []*domain.Service{
				{ID: "660e8400-e29b-41d4-a716-446655440001", Name: "Account Opening"},
				{ID: "660e8400-e29b-41d4-a716-446655440002", Name: "Vault Access"},
				{ID: "660e8400-e29b-41d4-a716-446655440003", Name: "Currency Exchange"},
			},
		},
	}

	for _, branch := range branches {
		r.branches[branch.ID] = branch
	}
}

func (r *BranchRepository) GetAll(_ context.Context) ([]*domain.Branch, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	branches := make([]*domain.Branch, 0, len(r.branches))
	for _, branch := range r.branches {
		branches = append(branches, branch)
	}
	return branches, nil
}

func (r *BranchRepository) GetByID(_ context.Context, id string) (*domain.Branch, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	branch, exists := r.branches[id]
	if !exists {
		return nil, domain.ErrBranchNotFound
	}
	return branch, nil
}

func (r *BranchRepository) GetServices(_ context.Context, branchID string) ([]*domain.Service, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	branch, exists := r.branches[branchID]
	if !exists {
		return nil, domain.ErrBranchNotFound
	}
	return branch.Services, nil
}

func (r *BranchRepository) Create(_ context.Context, branch *domain.Branch, serviceIDs []string) (*domain.Branch, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	knownServices := make(map[string]struct{})
	for _, existing := range r.branches {
		for _, service := range existing.Services {
			knownServices[service.ID] = struct{}{}
		}
	}
	for _, serviceID := range serviceIDs {
		if _, exists := knownServices[serviceID]; !exists {
			return nil, domain.ErrServiceNotFound
		}
	}

	if branch.ID == "" {
		branch.ID = uuid.New().String()
	}

	// Convert service IDs to service objects (simplified for memory repo)
	var services []*domain.Service
	for _, serviceID := range serviceIDs {
		services = append(services, &domain.Service{ID: serviceID, Name: "Service " + serviceID})
	}
	branch.Services = services

	r.branches[branch.ID] = branch
	return branch, nil
}

// Delete removes a branch from active lookup while retaining its archived record.
func (r *BranchRepository) Delete(_ context.Context, id string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	branch, exists := r.branches[id]
	if !exists {
		return 0, domain.ErrBranchNotFound
	}
	delete(r.branches, id)
	r.archived[id] = branch
	return 0, nil
}
