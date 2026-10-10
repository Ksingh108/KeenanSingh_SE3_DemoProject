package service

import (
	"testing"

	"github.com/GringottsBankBranchBooking/internal/repository/memory"
)

func TestServiceService_GetAllServices(t *testing.T) {
	serviceRepo := memory.NewServiceRepository()
	service := NewServiceService(serviceRepo)

	services, err := service.GetAllServices(nil)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if len(services) == 0 {
		t.Error("Expected services, got empty slice")
	}
}
