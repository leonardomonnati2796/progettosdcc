package registry

import (
	"context"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/leonardomonnati2796/distributed-service-registry/internal/storage"
	apiv1 "github.com/leonardomonnati2796/distributed-service-registry/pkg/api"
)

type ServiceRegistryServer struct {
	apiv1.UnimplementedServiceRegistryServer

	store        *storage.ServiceStore
	nodeID       string
	heartbeatTTL time.Duration
	now          func() time.Time
}

func NewServiceRegistryServer(store *storage.ServiceStore, nodeID string, heartbeatTTL time.Duration) *ServiceRegistryServer {
	if heartbeatTTL <= 0 {
		heartbeatTTL = 10 * time.Second
	}
	return &ServiceRegistryServer{
		store:        store,
		nodeID:       strings.TrimSpace(nodeID),
		heartbeatTTL: heartbeatTTL,
		now:          time.Now,
	}
}

func (s *ServiceRegistryServer) RegisterService(_ context.Context, req *apiv1.RegisterServiceRequest) (*apiv1.RegisterServiceResponse, error) {
	if req == nil || req.GetRecord() == nil {
		return nil, status.Error(codes.InvalidArgument, "record is required")
	}

	in := req.GetRecord()
	if strings.TrimSpace(in.GetServiceName()) == "" {
		return nil, status.Error(codes.InvalidArgument, "record.service_name is required")
	}
	if strings.TrimSpace(in.GetServiceId()) == "" {
		return nil, status.Error(codes.InvalidArgument, "record.service_id is required")
	}
	if strings.TrimSpace(in.GetEndpoint()) == "" {
		return nil, status.Error(codes.InvalidArgument, "record.endpoint is required")
	}
	if strings.TrimSpace(in.GetVersion()) == "" {
		return nil, status.Error(codes.InvalidArgument, "record.version is required")
	}

	nowUnix := s.now().Unix()
	healthStatus := in.GetHealthStatus()
	if healthStatus == apiv1.HealthStatus_HEALTH_STATUS_UNSPECIFIED {
		healthStatus = apiv1.HealthStatus_HEALTH_STATUS_SERVING
	}

	record := &apiv1.ServiceRecord{
		ServiceName:       strings.TrimSpace(in.GetServiceName()),
		ServiceId:         strings.TrimSpace(in.GetServiceId()),
		Endpoint:          strings.TrimSpace(in.GetEndpoint()),
		Version:           strings.TrimSpace(in.GetVersion()),
		HealthStatus:      healthStatus,
		LastHeartbeatUnix: in.GetLastHeartbeatUnix(),
		UpdatedAtUnix:     nowUnix,
		OwnerNodeId:       strings.TrimSpace(s.nodeID),
		LogicalVersion:    in.GetLogicalVersion(),
	}
	if record.LastHeartbeatUnix == 0 {
		record.LastHeartbeatUnix = nowUnix
	}

	stored := s.store.Upsert(record)
	return &apiv1.RegisterServiceResponse{
		Accepted: true,
		Message:  fmt.Sprintf("registered %s/%s %s at %s (version=%d)", stored.GetServiceName(), stored.GetServiceId(), stored.GetVersion(), stored.GetEndpoint(), stored.GetLogicalVersion()),
	}, nil
}

func (s *ServiceRegistryServer) DeregisterService(_ context.Context, req *apiv1.DeregisterServiceRequest) (*apiv1.DeregisterServiceResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	serviceName := strings.TrimSpace(req.GetServiceName())
	serviceID := strings.TrimSpace(req.GetServiceId())
	if serviceName == "" {
		return nil, status.Error(codes.InvalidArgument, "service_name is required")
	}
	if serviceID == "" {
		return nil, status.Error(codes.InvalidArgument, "service_id is required")
	}

	removed := s.store.Remove(serviceName, serviceID, s.now().Unix())
	if !removed {
		return &apiv1.DeregisterServiceResponse{Accepted: false, Message: "service not found"}, nil
	}
	return &apiv1.DeregisterServiceResponse{Accepted: true, Message: "service removed"}, nil
}

func (s *ServiceRegistryServer) Heartbeat(_ context.Context, req *apiv1.HeartbeatRequest) (*apiv1.HeartbeatResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	serviceName := strings.TrimSpace(req.GetServiceName())
	serviceID := strings.TrimSpace(req.GetServiceId())
	if serviceName == "" {
		return nil, status.Error(codes.InvalidArgument, "service_name is required")
	}
	if serviceID == "" {
		return nil, status.Error(codes.InvalidArgument, "service_id is required")
	}

	heartbeatUnix := req.GetHeartbeatUnix()
	if heartbeatUnix == 0 {
		heartbeatUnix = s.now().Unix()
	}

	healthStatus := req.GetHealthStatus()
	if healthStatus == apiv1.HealthStatus_HEALTH_STATUS_UNSPECIFIED {
		healthStatus = apiv1.HealthStatus_HEALTH_STATUS_SERVING
	}

	_, updated := s.store.UpdateHeartbeat(serviceName, serviceID, healthStatus, heartbeatUnix, s.now().Unix())
	if !updated {
		return &apiv1.HeartbeatResponse{Accepted: false, Message: "service not registered"}, nil
	}

	return &apiv1.HeartbeatResponse{Accepted: true, Message: "heartbeat accepted"}, nil
}

func (s *ServiceRegistryServer) GetService(_ context.Context, req *apiv1.GetServiceRequest) (*apiv1.GetServiceResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	serviceName := strings.TrimSpace(req.GetServiceName())
	if serviceName == "" {
		return nil, status.Error(codes.InvalidArgument, "service_name is required")
	}

	s.store.MarkStale(s.now().Unix(), int64(s.heartbeatTTL.Seconds()))
	records := s.store.Get(serviceName, req.GetServiceId())
	return &apiv1.GetServiceResponse{Records: records}, nil
}

func (s *ServiceRegistryServer) ListServices(_ context.Context, _ *apiv1.ListServicesRequest) (*apiv1.ListServicesResponse, error) {
	s.store.MarkStale(s.now().Unix(), int64(s.heartbeatTTL.Seconds()))
	records := s.store.List()
	return &apiv1.ListServicesResponse{Records: records}, nil
}

