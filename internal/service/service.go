package service

import (
	"context"

	"github.com/gringottsbankbranchbooking/internal/domain"
	"github.com/gringottsbankbranchbooking/internal/repository"
)

type ServiceService struct {
	serviceRepo repository.ServiceRepository
}

func NewServiceService(serviceRepo repository.ServiceRepository) *ServiceService {
	return &ServiceService{
		serviceRepo: serviceRepo,
	}
}

func (s *ServiceService) GetAllServices(ctx context.Context) ([]*domain.Service, error) {
	return s.serviceRepo.GetAll(ctx)
}
