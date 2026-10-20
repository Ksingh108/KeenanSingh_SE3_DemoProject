package service

import (
	"context"
	"testing"

	"github.com/gringottsbankbranchbooking/internal/repository/memory"
)

func TestServiceService_GetAllServices(t *testing.T) {
	serviceRepo := memory.NewServiceRepository()
	service := NewServiceService(serviceRepo)

	services, err := service.GetAllServices(context.Background())

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if len(services) == 0 {
		t.Error("Expected services, got empty slice")
	}
}
