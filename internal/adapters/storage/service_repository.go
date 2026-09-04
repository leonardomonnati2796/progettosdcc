package storage

import (
	"github.com/leonardomonnati2796/distributed-service-registry/internal/domain"
	legacy "github.com/leonardomonnati2796/distributed-service-registry/internal/storage"
	apiv1 "github.com/leonardomonnati2796/distributed-service-registry/pkg/api"
)

type ServiceRepository struct {
	store *legacy.ServiceStore
}

func NewServiceRepository(store *legacy.ServiceStore) *ServiceRepository {
	return &ServiceRepository{store: store}
}

func (repository *ServiceRepository) Register(service domain.Service) domain.Service {
	stored := repository.store.Upsert(toProto(service))
	return fromProto(stored)
}

func (repository *ServiceRepository) Deregister(name, id string, now int64) bool {
	return repository.store.Remove(name, id, now)
}

func (repository *ServiceRepository) Heartbeat(name, id string, health domain.HealthStatus, at int64) (domain.Service, bool) {
	stored, ok := repository.store.UpdateHeartbeat(name, id, toProtoHealth(health), at)
	if !ok {
		return domain.Service{}, false
	}
	return fromProto(stored), true
}

func (repository *ServiceRepository) Find(name, id string) []domain.Service {
	records := repository.store.Get(name, id)
	services := make([]domain.Service, 0, len(records))
	for _, record := range records {
		services = append(services, fromProto(record))
	}
	return services
}

func (repository *ServiceRepository) List() []domain.Service {
	records := repository.store.List()
	services := make([]domain.Service, 0, len(records))
	for _, record := range records {
		services = append(services, fromProto(record))
	}
	return services
}

func toProto(service domain.Service) *apiv1.ServiceRecord {
	return &apiv1.ServiceRecord{
		ServiceName:       service.Name,
		ServiceId:         service.ID,
		Endpoint:          service.Endpoint,
		Version:           service.Version,
		HealthStatus:      toProtoHealth(service.Health),
		LastHeartbeatUnix: service.LastHeartbeat,
		OwnerNodeId:       service.OwnerNode,
		LogicalVersion:    service.LogicalVersion,
	}
}

func fromProto(record *apiv1.ServiceRecord) domain.Service {
	if record == nil {
		return domain.Service{}
	}
	return domain.Service{
		Name:           record.GetServiceName(),
		ID:             record.GetServiceId(),
		Endpoint:       record.GetEndpoint(),
		Version:        record.GetVersion(),
		Health:         fromProtoHealth(record.GetHealthStatus()),
		LastHeartbeat:  record.GetLastHeartbeatUnix(),
		OwnerNode:      record.GetOwnerNodeId(),
		LogicalVersion: record.GetLogicalVersion(),
	}
}

func toProtoHealth(health domain.HealthStatus) apiv1.HealthStatus {
	switch health {
	case domain.HealthServing:
		return apiv1.HealthStatus_HEALTH_STATUS_SERVING
	case domain.HealthNotServing:
		return apiv1.HealthStatus_HEALTH_STATUS_NOT_SERVING
	case domain.HealthDegraded:
		return apiv1.HealthStatus_HEALTH_STATUS_DEGRADED
	default:
		return apiv1.HealthStatus_HEALTH_STATUS_UNSPECIFIED
	}
}

func fromProtoHealth(health apiv1.HealthStatus) domain.HealthStatus {
	switch health {
	case apiv1.HealthStatus_HEALTH_STATUS_SERVING:
		return domain.HealthServing
	case apiv1.HealthStatus_HEALTH_STATUS_NOT_SERVING:
		return domain.HealthNotServing
	case apiv1.HealthStatus_HEALTH_STATUS_DEGRADED:
		return domain.HealthDegraded
	default:
		return domain.HealthUnknown
	}
}
