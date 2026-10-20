package memory

import (
	"context"

	"github.com/gringottsbankbranchbooking/internal/domain"
)

type ServiceRepository struct {
	services map[string]*domain.Service
}

func NewServiceRepository() *ServiceRepository {
	repo := &ServiceRepository{
		services: make(map[string]*domain.Service),
	}
	repo.seedData()
	return repo
}

func (r *ServiceRepository) seedData() {
	services := []*domain.Service{
		{ID: "660e8400-e29b-41d4-a716-446655440001", Name: "Account Opening"},
		{ID: "660e8400-e29b-41d4-a716-446655440002", Name: "Vault Access"},
		{ID: "660e8400-e29b-41d4-a716-446655440003", Name: "Currency Exchange"},
		{ID: "660e8400-e29b-41d4-a716-446655440004", Name: "Loan Consultation"},
	}

	for _, service := range services {
		r.services[service.ID] = service
	}
}

func (r *ServiceRepository) GetAll(_ context.Context) ([]*domain.Service, error) {
	services := make([]*domain.Service, 0, len(r.services))
	for _, service := range r.services {
		services = append(services, service)
	}
	return services, nil
}
