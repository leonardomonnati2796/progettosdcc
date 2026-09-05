package application

import (
	"errors"
	"strings"

	"github.com/leonardomonnati2796/distributed-service-registry/internal/domain"
)

var (
	ErrInvalidService  = errors.New("service name and endpoint are required")
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
	if !ok || strings.TrimSpace(service.Endpoint) == "" {
		return domain.Service{}, ErrInvalidService
	}
	if service.Health == domain.HealthUnknown {
		service.Health = domain.HealthServing
	}
	return registry.repository.Register(service), nil
}

func (registry *ServiceRegistry) Deregister(name string, now int64) error {
	if strings.TrimSpace(name) == "" {
		return ErrInvalidService
	}
	if !registry.repository.Deregister(name, now) {
		return ErrServiceNotFound
	}
	return nil
}
