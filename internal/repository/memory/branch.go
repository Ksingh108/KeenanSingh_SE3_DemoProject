package memory

import (
	"context"
	"sync"

	"github.com/GringottsBankBranchBooking/internal/domain"
)

type BranchRepository struct {
	mu       sync.RWMutex
	branches map[string]*domain.Branch
}

func NewBranchRepository() *BranchRepository {
	repo := &BranchRepository{
		branches: make(map[string]*domain.Branch),
	}
	repo.seedData()
	return repo
}

func (r *BranchRepository) seedData() {
	branches := []*domain.Branch{
		{
			ID:       "550e8400-e29b-41d4-a716-446655440001",
			Name:     "Diagon Alley Main",
			Address:  "93 Diagon Alley",
			City:     "London",
			Phone:    "+44 20 7946 0958",
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
			City:     "London",
			Phone:    "+44 20 7946 0959",
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
			City:     "Hogsmeade",
			Phone:    "+44 1436 123456",
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

	if branch.ID == "" {
		branch.ID = "branch-" + branch.Name
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

// Delete removes a branch. The in-memory repositories are independent, so
// unlike PostgreSQL there is no cascade to bookings and the removed count is
// always 0.
func (r *BranchRepository) Delete(_ context.Context, id string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.branches[id]; !exists {
		return 0, domain.ErrBranchNotFound
	}
	delete(r.branches, id)
	return 0, nil
}
