package domain

import "strings"

type HealthStatus int

const (
	HealthUnknown HealthStatus = iota
	HealthServing
	HealthNotServing
	HealthDegraded
)

type Service struct {
	Name           string
	Endpoint       string
	Health         HealthStatus
	OwnerNode      string
	LogicalVersion uint64
}

func (service Service) Key() string {
	return strings.TrimSpace(service.Name)
}

func (service Service) IsTombstone() bool {
	return service.Health == HealthNotServing && service.Endpoint == ""
}

func NormalizeService(service Service) (Service, bool) {
	service.Name = strings.TrimSpace(service.Name)
	service.Endpoint = strings.TrimSpace(service.Endpoint)
	service.OwnerNode = strings.TrimSpace(service.OwnerNode)
	if service.Name == "" || service.Endpoint == "" {
		return Service{}, false
	}
	return service, true
}
