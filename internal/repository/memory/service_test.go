package memory

import (
	"context"
	"testing"
)

func TestNewServiceRepository(t *testing.T) {
	repo := NewServiceRepository()

	if repo == nil {
		t.Fatal("Expected repository to be created")
	}

	if repo.services == nil {
		t.Fatal("Expected services map to be initialized")
	}

	// Check seeded data
	if len(repo.services) == 0 {
		t.Fatal("Expected seeded services to exist")
	}

	// Verify specific seeded services
	expectedServices := []string{
		"660e8400-e29b-41d4-a716-446655440001", // Account Opening
		"660e8400-e29b-41d4-a716-446655440002", // Vault Access
		"660e8400-e29b-41d4-a716-446655440003", // Currency Exchange
		"660e8400-e29b-41d4-a716-446655440004", // Loan Consultation
	}

	for _, serviceID := range expectedServices {
		if _, exists := repo.services[serviceID]; !exists {
			t.Errorf("Expected seeded service %s to exist", serviceID)
		}
	}
}

func TestServiceRepository_GetAll(t *testing.T) {
	repo := NewServiceRepository()
	ctx := context.Background()

	services, err := repo.GetAll(ctx)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(services) != 4 {
		t.Errorf("Expected 4 services, got %d", len(services))
	}

	// Verify service names
	serviceNames := make(map[string]bool)
	for _, service := range services {
		serviceNames[service.Name] = true
	}

	expectedNames := []string{"Account Opening", "Vault Access", "Currency Exchange", "Loan Consultation"}
	for _, name := range expectedNames {
		if !serviceNames[name] {
			t.Errorf("Expected service '%s' to be present", name)
		}
	}
}
