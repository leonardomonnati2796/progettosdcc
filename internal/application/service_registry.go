package application

import (
	"errors"
	"strings"

	"github.com/leonardomonnati2796/distributed-service-registry/internal/domain"
)

var (
	ErrInvalidService  = errors.New("service name, id, endpoint and version are required")
	ErrServiceNotFound = errors.New("service not found")
)

type ServiceRegistry struct {
	repository domain.ServiceRepository
}

func NewServiceRegistry(repository domain.ServiceRepository) *ServiceRegistry {
	return &ServiceRegistry{repository: repository}
}

func (registry *ServiceRegistry) Register(service domain.Service) (domain.Service, error) {
	service, ok := domain.NormalizeService(service)
	if !ok || strings.TrimSpace(service.Endpoint) == "" || strings.TrimSpace(service.Version) == "" {
		return domain.Service{}, ErrInvalidService
	}
	if service.Health == domain.HealthUnknown {
		service.Health = domain.HealthServing
	}
	return registry.repository.Register(service), nil
}

func (registry *ServiceRegistry) Deregister(name, id string, now int64) error {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(id) == "" {
		return ErrInvalidService
	}
	if !registry.repository.Deregister(name, id, now) {
		return ErrServiceNotFound
	}
	return nil
}

func (registry *ServiceRegistry) Heartbeat(name, id string, health domain.HealthStatus, at int64) (domain.Service, error) {
	service, ok := registry.repository.Heartbeat(name, id, health, at)
	if !ok {
		return domain.Service{}, ErrServiceNotFound
	}
	return service, nil
}
