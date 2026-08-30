package registry

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
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

	registerCacheMu      sync.Mutex
	registerCache        map[string]*apiv1.RegisterServiceResponse
	registerCacheOrder   []string
	registerCacheCap     int
	deregisterCacheMu    sync.Mutex
	deregisterCache      map[string]*apiv1.DeregisterServiceResponse
	deregisterCacheOrder []string
	deregisterCacheCap   int
}

func NewServiceRegistryServer(store *storage.ServiceStore, nodeID string, heartbeatTTL time.Duration) *ServiceRegistryServer {
	// Crea un nuovo service registry server.
	if heartbeatTTL <= 0 {
		heartbeatTTL = 10 * time.Second
	}
	return &ServiceRegistryServer{
		store:              store,
		nodeID:             strings.TrimSpace(nodeID),
		heartbeatTTL:       heartbeatTTL,
		now:                time.Now,
		registerCache:      make(map[string]*apiv1.RegisterServiceResponse),
		registerCacheCap:   4096,
		deregisterCache:    make(map[string]*apiv1.DeregisterServiceResponse),
		deregisterCacheCap: 4096,
	}
}

func (s *ServiceRegistryServer) RegisterService(ctx context.Context, req *apiv1.RegisterServiceRequest) (*apiv1.RegisterServiceResponse, error) {
	// Registra service.
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

	return s.registerServiceRecord(ctx, in)
}

func (s *ServiceRegistryServer) registerServiceRecord(ctx context.Context, in *apiv1.ServiceRecord) (*apiv1.RegisterServiceResponse, error) {
	// Registra service record.
	requestID := extractRequestIDFromContext(ctx)
	if requestID != "" {
		if cached, ok := s.getCachedRegisterResponse(requestID); ok {
			return cached, nil
		}
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
		OwnerNodeId:       strings.TrimSpace(s.nodeID),
		LogicalVersion:    in.GetLogicalVersion(),
	}
	if record.LastHeartbeatUnix == 0 {
		record.LastHeartbeatUnix = nowUnix
	}

	if existing, ok := s.getActiveRecord(record.GetServiceName(), record.GetServiceId()); ok && isEquivalentRegisterRecord(existing, record) {
		response := &apiv1.RegisterServiceResponse{
			Accepted: true,
			Message:  fmt.Sprintf("already registered %s/%s %s at %s (version=%d)", existing.GetServiceName(), existing.GetServiceId(), existing.GetVersion(), existing.GetEndpoint(), existing.GetLogicalVersion()),
		}
		s.cacheRegisterResponse(requestID, response)
		return response, nil
	}

	stored := s.store.Upsert(record)

	response := &apiv1.RegisterServiceResponse{
		Accepted: true,
		Message:  fmt.Sprintf("registered %s/%s %s at %s (version=%d)", stored.GetServiceName(), stored.GetServiceId(), stored.GetVersion(), stored.GetEndpoint(), stored.GetLogicalVersion()),
	}
	s.cacheRegisterResponse(requestID, response)
	return response, nil
}

func (s *ServiceRegistryServer) getActiveRecord(serviceName string, serviceID string) (*apiv1.ServiceRecord, bool) {
	// Recupera active record.
	records := s.store.Get(serviceName, serviceID)
	if len(records) == 0 || records[0] == nil {
		return nil, false
	}
	return records[0], true
}

func isEquivalentRegisterRecord(existing *apiv1.ServiceRecord, incoming *apiv1.ServiceRecord) bool {
	// Verifica la condizione richiesta.
	if existing == nil || incoming == nil {
		return false
	}
	return existing.GetServiceName() == incoming.GetServiceName() &&
		existing.GetServiceId() == incoming.GetServiceId() &&
		existing.GetEndpoint() == incoming.GetEndpoint() &&
		existing.GetVersion() == incoming.GetVersion() &&
		existing.GetHealthStatus() == incoming.GetHealthStatus()
}

func (s *ServiceRegistryServer) getCachedRegisterResponse(requestID string) (*apiv1.RegisterServiceResponse, bool) {
	// Recupera cached register response.
	if requestID == "" {
		return nil, false
	}

	s.registerCacheMu.Lock()
	defer s.registerCacheMu.Unlock()

	resp, ok := s.registerCache[requestID]
	if !ok || resp == nil {
		return nil, false
	}
	return &apiv1.RegisterServiceResponse{Accepted: resp.GetAccepted(), Message: resp.GetMessage()}, true
}

func (s *ServiceRegistryServer) cacheRegisterResponse(requestID string, response *apiv1.RegisterServiceResponse) {
	// Esegue la logica di cache register response.
	if requestID == "" || response == nil || s.registerCacheCap <= 0 {
		return
	}

	s.registerCacheMu.Lock()
	defer s.registerCacheMu.Unlock()

	if _, exists := s.registerCache[requestID]; !exists {
		s.registerCacheOrder = append(s.registerCacheOrder, requestID)
	}
	s.registerCache[requestID] = &apiv1.RegisterServiceResponse{Accepted: response.GetAccepted(), Message: response.GetMessage()}

	for len(s.registerCacheOrder) > s.registerCacheCap {
		evict := s.registerCacheOrder[0]
		s.registerCacheOrder = s.registerCacheOrder[1:]
		delete(s.registerCache, evict)
	}
}

func extractRequestIDFromContext(ctx context.Context) string {
	// Esegue la logica di extract request idfrom context.
	if ctx == nil {
		return ""
	}
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	values := md.Get("x-request-id")
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[0])
}

func (s *ServiceRegistryServer) DeregisterService(ctx context.Context, req *apiv1.DeregisterServiceRequest) (*apiv1.DeregisterServiceResponse, error) {
	// Deregistra service.
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

	requestID := extractRequestIDFromContext(ctx)
	if requestID != "" {
		if cached, ok := s.getCachedDeregisterResponse(requestID); ok {
			return cached, nil
		}
		if persisted, ok := s.store.GetDeregisterResult(requestID); ok {
			return persisted, nil
		}
	}

	removed := s.store.Remove(serviceName, serviceID, s.now().Unix())
	if !removed {
		resp := &apiv1.DeregisterServiceResponse{Accepted: false, Message: "service not found"}
		s.store.RecordDeregisterResult(requestID, resp)
		s.cacheDeregisterResponse(requestID, resp)
		return resp, nil
	}
	resp := &apiv1.DeregisterServiceResponse{Accepted: true, Message: "service removed"}
	s.store.RecordDeregisterResult(requestID, resp)
	s.cacheDeregisterResponse(requestID, resp)
	return resp, nil
}

func (s *ServiceRegistryServer) getCachedDeregisterResponse(requestID string) (*apiv1.DeregisterServiceResponse, bool) {
	// Recupera cached deregister response.
	if requestID == "" {
		return nil, false
	}

	s.deregisterCacheMu.Lock()
	defer s.deregisterCacheMu.Unlock()

	resp, ok := s.deregisterCache[requestID]
	if !ok || resp == nil {
		return nil, false
	}
	return &apiv1.DeregisterServiceResponse{Accepted: resp.GetAccepted(), Message: resp.GetMessage()}, true
}

func (s *ServiceRegistryServer) cacheDeregisterResponse(requestID string, response *apiv1.DeregisterServiceResponse) {
	// Esegue la logica di cache deregister response.
	if requestID == "" || response == nil || s.deregisterCacheCap <= 0 {
		return
	}

	s.deregisterCacheMu.Lock()
	defer s.deregisterCacheMu.Unlock()

	if _, exists := s.deregisterCache[requestID]; !exists {
		s.deregisterCacheOrder = append(s.deregisterCacheOrder, requestID)
	}
	s.deregisterCache[requestID] = &apiv1.DeregisterServiceResponse{Accepted: response.GetAccepted(), Message: response.GetMessage()}

	for len(s.deregisterCacheOrder) > s.deregisterCacheCap {
		evict := s.deregisterCacheOrder[0]
		s.deregisterCacheOrder = s.deregisterCacheOrder[1:]
		delete(s.deregisterCache, evict)
	}
}

func (s *ServiceRegistryServer) Heartbeat(_ context.Context, req *apiv1.HeartbeatRequest) (*apiv1.HeartbeatResponse, error) {
	// Gestisce il heartbeat del servizio.
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

	_, updated := s.store.UpdateHeartbeat(serviceName, serviceID, healthStatus, heartbeatUnix)
	if !updated {
		return &apiv1.HeartbeatResponse{Accepted: false, Message: "service not registered"}, nil
	}

	return &apiv1.HeartbeatResponse{Accepted: true, Message: "heartbeat accepted"}, nil
}

func (s *ServiceRegistryServer) GetService(_ context.Context, req *apiv1.GetServiceRequest) (*apiv1.GetServiceResponse, error) {
	// Recupera service.
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
	// Elenca services.
	s.store.MarkStale(s.now().Unix(), int64(s.heartbeatTTL.Seconds()))
	records := s.store.List()
	return &apiv1.ListServicesResponse{Records: records}, nil
}
