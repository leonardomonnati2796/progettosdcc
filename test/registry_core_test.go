package test

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/leonardomonnati2796/distributed-service-registry/internal/registry"
	"github.com/leonardomonnati2796/distributed-service-registry/internal/storage"
	apiv1 "github.com/leonardomonnati2796/distributed-service-registry/pkg/api"
)

func TestServiceRegistryServerRegisterAndHeartbeatAreVisible(t *testing.T) {
// Esegue il test per service registry server register and heartbeat are visible.
	store := storage.NewServiceStore()
	srv := registry.NewServiceRegistryServer(store, "node-1", 5*time.Second)

	registerResp, err := srv.RegisterService(context.Background(), &apiv1.RegisterServiceRequest{
		Record: &apiv1.ServiceRecord{
			ServiceName: "users",
			ServiceId:   "users-1",
			Endpoint:    "users-1:8080",
			Version:     "v1.0.0",
		},
	})
	if err != nil {
		t.Fatalf("register returned error: %v", err)
	}
	if !registerResp.GetAccepted() {
		t.Fatalf("expected register to be accepted")
	}

	heartbeatResp, err := srv.Heartbeat(context.Background(), &apiv1.HeartbeatRequest{
		ServiceName:  "users",
		ServiceId:    "users-1",
		HealthStatus: apiv1.HealthStatus_HEALTH_STATUS_DEGRADED,
	})
	if err != nil {
		t.Fatalf("heartbeat returned error: %v", err)
	}
	if !heartbeatResp.GetAccepted() {
		t.Fatalf("expected heartbeat to be accepted")
	}

	getResp, err := srv.GetService(context.Background(), &apiv1.GetServiceRequest{ServiceName: "users", ServiceId: "users-1"})
	if err != nil {
		t.Fatalf("get returned error: %v", err)
	}
	if len(getResp.GetRecords()) != 1 {
		t.Fatalf("expected one matching record, got %d", len(getResp.GetRecords()))
	}
	if got := getResp.GetRecords()[0].GetHealthStatus(); got != apiv1.HealthStatus_HEALTH_STATUS_DEGRADED {
		t.Fatalf("expected health status DEGRADED after heartbeat, got %v", got)
	}
}

func TestServiceRegistryServerDeregisterRoundTrip(t *testing.T) {
// Esegue il test per service registry server deregister round trip.
	store := storage.NewServiceStore()
	srv := registry.NewServiceRegistryServer(store, "node-1", 5*time.Second)

	_, err := srv.RegisterService(context.Background(), &apiv1.RegisterServiceRequest{
		Record: &apiv1.ServiceRecord{
			ServiceName: "orders",
			ServiceId:   "orders-1",
			Endpoint:    "orders-1:8080",
			Version:     "v1",
		},
	})
	if err != nil {
		t.Fatalf("register returned error: %v", err)
	}

	removeResp, err := srv.DeregisterService(context.Background(), &apiv1.DeregisterServiceRequest{ServiceName: "orders", ServiceId: "orders-1"})
	if err != nil {
		t.Fatalf("deregister returned error: %v", err)
	}
	if !removeResp.GetAccepted() {
		t.Fatalf("expected deregister accepted=true")
	}

	getResp, err := srv.GetService(context.Background(), &apiv1.GetServiceRequest{ServiceName: "orders", ServiceId: "orders-1"})
	if err != nil {
		t.Fatalf("get returned error: %v", err)
	}
	if len(getResp.GetRecords()) != 0 {
		t.Fatalf("expected no records after deregister, got %d", len(getResp.GetRecords()))
	}
}

func TestServiceRegistryServerRegisterValidation(t *testing.T) {
// Esegue il test per service registry server register validation.
	srv := registry.NewServiceRegistryServer(storage.NewServiceStore(), "node-1", 5*time.Second)

	_, err := srv.RegisterService(context.Background(), &apiv1.RegisterServiceRequest{Record: &apiv1.ServiceRecord{}})
	if err == nil {
		t.Fatalf("expected validation error")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected grpc status error")
	}
	if st.Code() != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument, got %s", st.Code().String())
	}
}

func TestServiceRegistryServerRegisterIdempotentWithRequestID(t *testing.T) {
// Esegue il test per service registry server register idempotent with request id.
	store := storage.NewServiceStore()
	srv := registry.NewServiceRegistryServer(store, "node-1", 5*time.Second)

	req := &apiv1.RegisterServiceRequest{
		Record: &apiv1.ServiceRecord{
			ServiceName: "users",
			ServiceId:   "users-1",
			Endpoint:    "users-1:8080",
			Version:     "v1.0.0",
		},
	}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-request-id", "req-users-1"))

	if _, err := srv.RegisterService(ctx, req); err != nil {
		t.Fatalf("first register returned error: %v", err)
	}
	if _, err := srv.RegisterService(ctx, req); err != nil {
		t.Fatalf("second register returned error: %v", err)
	}

	getResp, err := srv.GetService(context.Background(), &apiv1.GetServiceRequest{ServiceName: "users", ServiceId: "users-1"})
	if err != nil {
		t.Fatalf("get returned error: %v", err)
	}
	if len(getResp.GetRecords()) != 1 {
		t.Fatalf("expected one record, got %d", len(getResp.GetRecords()))
	}
	if got := getResp.GetRecords()[0].GetLogicalVersion(); got != 1 {
		t.Fatalf("expected logical version 1 on duplicate request, got %d", got)
	}
}
