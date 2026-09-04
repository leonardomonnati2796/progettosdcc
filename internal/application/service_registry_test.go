package application

import (
	"testing"

	"github.com/leonardomonnati2796/distributed-service-registry/internal/domain"
)

type memoryServiceRepository struct {
	services map[string]domain.Service
}

func (repository *memoryServiceRepository) Register(service domain.Service) domain.Service {
	if service.LogicalVersion == 0 {
		service.LogicalVersion = 1
	}
	repository.services[service.Key()] = service
	return service
}

func (repository *memoryServiceRepository) Deregister(name, id string, _ int64) bool {
	key := domain.Service{Name: name, ID: id}.Key()
	if _, ok := repository.services[key]; !ok {
		return false
	}
	delete(repository.services, key)
	return true
}

func (repository *memoryServiceRepository) Heartbeat(name, id string, health domain.HealthStatus, at int64) (domain.Service, bool) {
	key := domain.Service{Name: name, ID: id}.Key()
	service, ok := repository.services[key]
	if !ok {
		return domain.Service{}, false
	}
	service.Health = health
	service.LastHeartbeat = at
	repository.services[key] = service
	return service, true
}

func (repository *memoryServiceRepository) Find(name, id string) []domain.Service {
	service, ok := repository.services[(domain.Service{Name: name, ID: id}).Key()]
	if !ok {
		return nil
	}
	return []domain.Service{service}
}

func (repository *memoryServiceRepository) List() []domain.Service {
	services := make([]domain.Service, 0, len(repository.services))
	for _, service := range repository.services {
		services = append(services, service)
	}
	return services
}

func TestServiceRegistryNormalizesAndAppliesDefaults(t *testing.T) {
	repository := &memoryServiceRepository{services: make(map[string]domain.Service)}
	registry := NewServiceRegistry(repository)

	service, err := registry.Register(domain.Service{
		Name:     " users ",
		ID:       " users-1 ",
		Endpoint: " users:8080 ",
		Version:  " v1 ",
	})
	if err != nil {
		t.Fatalf("register returned error: %v", err)
	}
	if service.Name != "users" || service.ID != "users-1" {
		t.Fatalf("service identity was not normalized: %+v", service)
	}
	if service.Health != domain.HealthServing {
		t.Fatalf("expected serving default, got %v", service.Health)
	}
	if service.LogicalVersion != 1 {
		t.Fatalf("expected logical version 1, got %d", service.LogicalVersion)
	}
}

func TestServiceRegistryRejectsIncompleteService(t *testing.T) {
	registry := NewServiceRegistry(&memoryServiceRepository{services: make(map[string]domain.Service)})

	if _, err := registry.Register(domain.Service{Name: "users"}); err != ErrInvalidService {
		t.Fatalf("expected ErrInvalidService, got %v", err)
	}
}
