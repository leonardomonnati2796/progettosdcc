package client

import (
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"

	apiv1 "github.com/leonardomonnati2796/distributed-service-registry/pkg/api"
)

type fakeRegistryServer struct {
	apiv1.UnimplementedServiceRegistryServer

	mu sync.Mutex

	registerAccepted   bool
	heartbeatAccepted  bool
	deregisterAccepted bool

	registerCalls   int
	heartbeatCalls  int
	deregisterCalls int

	lastHeartbeatUnix int64
}

func (s *fakeRegistryServer) RegisterService(_ context.Context, _ *apiv1.RegisterServiceRequest) (*apiv1.RegisterServiceResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.registerCalls++
	if !s.registerAccepted {
		return &apiv1.RegisterServiceResponse{Accepted: false, Message: "register rejected by test"}, nil
	}
	return &apiv1.RegisterServiceResponse{Accepted: true}, nil
}

func (s *fakeRegistryServer) Heartbeat(_ context.Context, req *apiv1.HeartbeatRequest) (*apiv1.HeartbeatResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.heartbeatCalls++
	s.lastHeartbeatUnix = req.GetHeartbeatUnix()
	if !s.heartbeatAccepted {
		return &apiv1.HeartbeatResponse{Accepted: false, Message: "heartbeat rejected by test"}, nil
	}
	return &apiv1.HeartbeatResponse{Accepted: true}, nil
}

func (s *fakeRegistryServer) DeregisterService(_ context.Context, _ *apiv1.DeregisterServiceRequest) (*apiv1.DeregisterServiceResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.deregisterCalls++
	if !s.deregisterAccepted {
		return &apiv1.DeregisterServiceResponse{Accepted: false, Message: "deregister rejected by test"}, nil
	}
	return &apiv1.DeregisterServiceResponse{Accepted: true}, nil
}

func (s *fakeRegistryServer) GetService(context.Context, *apiv1.GetServiceRequest) (*apiv1.GetServiceResponse, error) {
	return &apiv1.GetServiceResponse{}, nil
}

func (s *fakeRegistryServer) ListServices(context.Context, *apiv1.ListServicesRequest) (*apiv1.ListServicesResponse, error) {
	return &apiv1.ListServicesResponse{}, nil
}

func startFakeRegistryServer(t *testing.T, srv apiv1.ServiceRegistryServer) (address string, stop func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}

	grpcServer := grpc.NewServer()
	apiv1.RegisterServiceRegistryServer(grpcServer, srv)

	go func() {
		_ = grpcServer.Serve(listener)
	}()

	return listener.Addr().String(), func() {
		grpcServer.GracefulStop()
		_ = listener.Close()
	}
}

func TestRegistryClientRegisterHeartbeatDeregisterSuccess(t *testing.T) {
	fake := &fakeRegistryServer{
		registerAccepted:   true,
		heartbeatAccepted:  true,
		deregisterAccepted: true,
	}
	address, stop := startFakeRegistryServer(t, fake)
	defer stop()

	c := NewRegistryClient([]string{address}, 500*time.Millisecond, 500*time.Millisecond)

	err := c.Register(&apiv1.ServiceRecord{
		ServiceName: "users",
		ServiceId:   "users-1",
		Endpoint:    "users-1:8080",
		Version:     "v1.0.0",
	})
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	if err := c.Heartbeat("users", "users-1", apiv1.HealthStatus_HEALTH_STATUS_SERVING); err != nil {
		t.Fatalf("heartbeat failed: %v", err)
	}
	if err := c.Deregister("users", "users-1"); err != nil {
		t.Fatalf("deregister failed: %v", err)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.registerCalls != 1 {
		t.Fatalf("expected 1 register call, got %d", fake.registerCalls)
	}
	if fake.heartbeatCalls != 1 {
		t.Fatalf("expected 1 heartbeat call, got %d", fake.heartbeatCalls)
	}
	if fake.deregisterCalls != 1 {
		t.Fatalf("expected 1 deregister call, got %d", fake.deregisterCalls)
	}
	if fake.lastHeartbeatUnix == 0 {
		t.Fatalf("expected heartbeat unix timestamp to be set")
	}
}

func TestRegistryClientFailoverWithOneUnreachableEndpoint(t *testing.T) {
	fake := &fakeRegistryServer{
		registerAccepted:   true,
		heartbeatAccepted:  true,
		deregisterAccepted: true,
	}
	address, stop := startFakeRegistryServer(t, fake)
	defer stop()

	c := NewRegistryClient(
		[]string{"127.0.0.1:1", address},
		150*time.Millisecond,
		500*time.Millisecond,
	)

	err := c.Register(&apiv1.ServiceRecord{
		ServiceName: "orders",
		ServiceId:   "orders-1",
		Endpoint:    "orders-1:8080",
		Version:     "v1.0.0",
	})
	if err != nil {
		t.Fatalf("expected failover register success, got error: %v", err)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.registerCalls != 1 {
		t.Fatalf("expected exactly one successful register call to reachable endpoint, got %d", fake.registerCalls)
	}
}

func TestRegistryClientRejectAndNoEndpointsErrors(t *testing.T) {
	fake := &fakeRegistryServer{
		registerAccepted:   false,
		heartbeatAccepted:  true,
		deregisterAccepted: true,
	}
	address, stop := startFakeRegistryServer(t, fake)
	defer stop()

	c := NewRegistryClient([]string{address}, 500*time.Millisecond, 500*time.Millisecond)
	err := c.Register(&apiv1.ServiceRecord{
		ServiceName: "users",
		ServiceId:   "users-2",
		Endpoint:    "users-2:8080",
		Version:     "v1.0.0",
	})
	if err == nil {
		t.Fatalf("expected register rejection error")
	}
	if !strings.Contains(err.Error(), "RegisterService rejected") {
		t.Fatalf("unexpected error message: %v", err)
	}

	emptyClient := NewRegistryClient(nil, 500*time.Millisecond, 500*time.Millisecond)
	err = emptyClient.Deregister("users", "users-2")
	if err == nil {
		t.Fatalf("expected no endpoints error")
	}
	if !strings.Contains(err.Error(), "no registry endpoints configured") {
		t.Fatalf("unexpected no-endpoints error: %v", err)
	}
}

