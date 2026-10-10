package service

import (
	"context"

	"github.com/GringottsBankBranchBooking/internal/domain"
	"github.com/GringottsBankBranchBooking/internal/repository"
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
