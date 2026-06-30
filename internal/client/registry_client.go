package client

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	apiv1 "github.com/leonardomonnati2796/distributed-service-registry/pkg/api"
)

// RegistryClient wraps multiple registry endpoints with random-order failover.
// Each RPC call dials a fresh connection to one of the configured endpoints,
// trying others in random order if the first attempt fails.
type RegistryClient struct {
	endpoints   []string
	dialTimeout time.Duration
	rpcTimeout  time.Duration
}

// NewRegistryClient creates a client targeting the given endpoints.
// dialTimeout controls how long to wait for a TCP connection to be established;
// rpcTimeout controls how long to wait for an individual RPC to complete.
func NewRegistryClient(endpoints []string, dialTimeout, rpcTimeout time.Duration) *RegistryClient {
	return &RegistryClient{
		endpoints:   endpoints,
		dialTimeout: dialTimeout,
		rpcTimeout:  rpcTimeout,
	}
}

// withClient tries each endpoint in random order, dialing and calling fn.
// Returns nil on the first success, or the last error if all fail.
func (c *RegistryClient) withClient(fn func(apiv1.ServiceRegistryClient) error) error {
	if len(c.endpoints) == 0 {
		return fmt.Errorf("no registry endpoints configured")
	}

	order := rand.Perm(len(c.endpoints))
	var lastErr error
	for _, idx := range order {
		addr := c.endpoints[idx]

		dialCtx, dialCancel := context.WithTimeout(context.Background(), c.dialTimeout)
		conn, err := grpc.DialContext( //nolint:staticcheck // consistent with existing codebase
			dialCtx,
			addr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithBlock(),
		)
		dialCancel()
		if err != nil {
			lastErr = fmt.Errorf("dial %s: %w", addr, err)
			continue
		}

		stub := apiv1.NewServiceRegistryClient(conn)
		callErr := fn(stub)
		conn.Close()
		if callErr != nil {
			lastErr = callErr
			continue
		}
		return nil
	}
	return lastErr
}

// Register sends a RegisterService RPC to one of the registry endpoints.
func (c *RegistryClient) Register(record *apiv1.ServiceRecord) error {
	return c.withClient(func(stub apiv1.ServiceRegistryClient) error {
		ctx, cancel := context.WithTimeout(context.Background(), c.rpcTimeout)
		defer cancel()
		resp, err := stub.RegisterService(ctx, &apiv1.RegisterServiceRequest{Record: record})
		if err != nil {
			return fmt.Errorf("RegisterService RPC: %w", err)
		}
		if !resp.GetAccepted() {
			return fmt.Errorf("RegisterService rejected: %s", resp.GetMessage())
		}
		return nil
	})
}

// Heartbeat sends a Heartbeat RPC for the given service instance.
func (c *RegistryClient) Heartbeat(serviceName, serviceID string, status apiv1.HealthStatus) error {
	return c.withClient(func(stub apiv1.ServiceRegistryClient) error {
		ctx, cancel := context.WithTimeout(context.Background(), c.rpcTimeout)
		defer cancel()
		resp, err := stub.Heartbeat(ctx, &apiv1.HeartbeatRequest{
			ServiceName:   serviceName,
			ServiceId:     serviceID,
			HealthStatus:  status,
			HeartbeatUnix: time.Now().Unix(),
		})
		if err != nil {
			return fmt.Errorf("Heartbeat RPC: %w", err)
		}
		if !resp.GetAccepted() {
			return fmt.Errorf("Heartbeat rejected: %s", resp.GetMessage())
		}
		return nil
	})
}

// Deregister sends a DeregisterService RPC for the given service instance.
func (c *RegistryClient) Deregister(serviceName, serviceID string) error {
	return c.withClient(func(stub apiv1.ServiceRegistryClient) error {
		ctx, cancel := context.WithTimeout(context.Background(), c.rpcTimeout)
		defer cancel()
		resp, err := stub.DeregisterService(ctx, &apiv1.DeregisterServiceRequest{
			ServiceName: serviceName,
			ServiceId:   serviceID,
		})
		if err != nil {
			return fmt.Errorf("DeregisterService RPC: %w", err)
		}
		if !resp.GetAccepted() {
			return fmt.Errorf("DeregisterService rejected: %s", resp.GetMessage())
		}
		return nil
	})
}

// Get returns the registry records for the given service name and optional id.
func (c *RegistryClient) Get(serviceName, serviceID string) ([]*apiv1.ServiceRecord, error) {
	var records []*apiv1.ServiceRecord
	err := c.withClient(func(stub apiv1.ServiceRegistryClient) error {
		ctx, cancel := context.WithTimeout(context.Background(), c.rpcTimeout)
		defer cancel()
		resp, err := stub.GetService(ctx, &apiv1.GetServiceRequest{
			ServiceName: serviceName,
			ServiceId:   serviceID,
		})
		if err != nil {
			return fmt.Errorf("GetService RPC: %w", err)
		}
		records = resp.GetRecords()
		return nil
	})
	if err != nil {
		return nil, err
	}
	return records, nil
}

// List returns all registry records visible from one of the configured endpoints.
func (c *RegistryClient) List() ([]*apiv1.ServiceRecord, error) {
	var records []*apiv1.ServiceRecord
	err := c.withClient(func(stub apiv1.ServiceRegistryClient) error {
		ctx, cancel := context.WithTimeout(context.Background(), c.rpcTimeout)
		defer cancel()
		resp, err := stub.ListServices(ctx, &apiv1.ListServicesRequest{})
		if err != nil {
			return fmt.Errorf("ListServices RPC: %w", err)
		}
		records = resp.GetRecords()
		return nil
	})
	if err != nil {
		return nil, err
	}
	return records, nil
}

