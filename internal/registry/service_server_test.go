package registry

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/leonardomonnati2796/distributed-service-registry/internal/storage"
	apiv1 "github.com/leonardomonnati2796/distributed-service-registry/pkg/api"
)

func TestServiceRegistryServerRegisterHeartbeatAndStale(t *testing.T) {
	store := storage.NewServiceStore()
	srv := NewServiceRegistryServer(store, "node-1", 5*time.Second)
	now := int64(1000)
	srv.now = func() time.Time { return time.Unix(now, 0) }

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
		t.Fatalf("expected register accepted=true")
	}

	now = 1003
	heartbeatResp, err := srv.Heartbeat(context.Background(), &apiv1.HeartbeatRequest{
		ServiceName:   "users",
		ServiceId:     "users-1",
		HealthStatus:  apiv1.HealthStatus_HEALTH_STATUS_DEGRADED,
		HeartbeatUnix: 1003,
	})
	if err != nil {
		t.Fatalf("heartbeat returned error: %v", err)
	}
	if !heartbeatResp.GetAccepted() {
		t.Fatalf("expected heartbeat accepted=true")
	}

	now = 1004
	getResp, err := srv.GetService(context.Background(), &apiv1.GetServiceRequest{ServiceName: "users", ServiceId: "users-1"})
	if err != nil {
		t.Fatalf("get returned error: %v", err)
	}
	if len(getResp.GetRecords()) != 1 {
		t.Fatalf("expected 1 record, got %d", len(getResp.GetRecords()))
	}
	if getResp.GetRecords()[0].GetHealthStatus() != apiv1.HealthStatus_HEALTH_STATUS_DEGRADED {
		t.Fatalf("expected health to stay DEGRADED before TTL expiration")
	}

	now = 1010
	getAfterTTL, err := srv.GetService(context.Background(), &apiv1.GetServiceRequest{ServiceName: "users", ServiceId: "users-1"})
	if err != nil {
		t.Fatalf("get after ttl returned error: %v", err)
	}
	if getAfterTTL.GetRecords()[0].GetHealthStatus() != apiv1.HealthStatus_HEALTH_STATUS_NOT_SERVING {
		t.Fatalf("expected health NOT_SERVING after heartbeat TTL")
	}
}

func TestServiceRegistryServerDeregister(t *testing.T) {
	store := storage.NewServiceStore()
	srv := NewServiceRegistryServer(store, "node-1", 5*time.Second)
	srv.now = func() time.Time { return time.Unix(2000, 0) }

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

	notFoundResp, err := srv.DeregisterService(context.Background(), &apiv1.DeregisterServiceRequest{ServiceName: "orders", ServiceId: "orders-1"})
	if err != nil {
		t.Fatalf("second deregister returned error: %v", err)
	}
	if notFoundResp.GetAccepted() {
		t.Fatalf("expected second deregister accepted=false")
	}
}

func TestServiceRegistryServerRegisterValidation(t *testing.T) {
	srv := NewServiceRegistryServer(storage.NewServiceStore(), "node-1", 5*time.Second)

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

