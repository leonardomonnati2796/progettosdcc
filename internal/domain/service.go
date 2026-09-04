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
	ID             string
	Endpoint       string
	Version        string
	Health         HealthStatus
	LastHeartbeat  int64
	OwnerNode      string
	LogicalVersion uint64
}

func (service Service) Key() string {
	return strings.TrimSpace(service.Name) + "|" + strings.TrimSpace(service.ID)
}

func (service Service) IsTombstone() bool {
	return service.Health == HealthNotServing && service.Endpoint == "" && service.Version == ""
}

func NormalizeService(service Service) (Service, bool) {
	service.Name = strings.TrimSpace(service.Name)
	service.ID = strings.TrimSpace(service.ID)
	service.Endpoint = strings.TrimSpace(service.Endpoint)
	service.Version = strings.TrimSpace(service.Version)
	service.OwnerNode = strings.TrimSpace(service.OwnerNode)
	if service.Name == "" || service.ID == "" {
		return Service{}, false
	}
	return service, true
}
