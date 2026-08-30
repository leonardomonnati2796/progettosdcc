package client

import (
	"context"
	crand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	apiv1 "github.com/leonardomonnati2796/distributed-service-registry/pkg/api"
)

// RegistryClient wraps multiple registry endpoints with random-order failover.
// Each RPC call dials a fresh connection to one of the configured endpoints,
// trying others in random order if the first attempt fails.
type RegistryClient struct {
	endpoints        []string
	dialTimeout      time.Duration
	rpcTimeout       time.Duration
	registerTrace    bool
	registerTraceOut io.Writer
}

const (
	registerProbeTimeout = 2 * time.Second
	registerAckTimeout   = 5 * time.Second
	registerRetryDelay   = 200 * time.Millisecond
)

// NewRegistryClient creates a client targeting the given endpoints.
// dialTimeout controls how long to wait for a TCP connection to be established;
// rpcTimeout controls how long to wait for an individual RPC to complete.
func NewRegistryClient(endpoints []string, dialTimeout, rpcTimeout time.Duration) *RegistryClient {
	// Crea un nuovo registry client.
	return &RegistryClient{
		endpoints:        endpoints,
		dialTimeout:      dialTimeout,
		rpcTimeout:       rpcTimeout,
		registerTraceOut: os.Stderr,
	}
}

// SetRegisterTrace enables/disables trace logs for the register workflow.
func (c *RegistryClient) SetRegisterTrace(enabled bool, writer io.Writer) {
	// Imposta register trace.
	c.registerTrace = enabled
	if writer != nil {
		c.registerTraceOut = writer
	}
}

// withClient tries each endpoint in random order, dialing and calling fn.
// Returns nil on the first success, or the last error if all fail.
func (c *RegistryClient) withClient(fn func(apiv1.ServiceRegistryClient) error) error {
	// Esegue la logica di with client.
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
	// Registra esegue la logica della funzione..
	_, err := c.RegisterWithResponse(record)
	return err
}

// RegisterWithResponse sends a RegisterService RPC and returns the server response.
func (c *RegistryClient) RegisterWithResponse(record *apiv1.ServiceRecord) (*apiv1.RegisterServiceResponse, error) {
	// Registra with response.
	if len(c.endpoints) == 0 {
		return nil, fmt.Errorf("no registry endpoints configured")
	}

	requestID := newRequestID()
	attempt := 1
	for {
		c.traceRegisterf("register attempt %d (request_id=%s): probing nodes (timeout=%s)", attempt, requestID, registerProbeTimeout)
		responsive := c.probeResponsiveEndpoints(registerProbeTimeout)
		if len(responsive) == 0 {
			c.traceRegisterf("register attempt %d (request_id=%s): no nodes responded within %s; retrying", attempt, requestID, registerProbeTimeout)
			attempt++
			time.Sleep(registerRetryDelay)
			continue
		}

		c.traceRegisterf("register attempt %d (request_id=%s): responsive nodes=%s", attempt, requestID, strings.Join(responsive, ","))
		target := responsive[rand.Intn(len(responsive))]
		c.traceRegisterf("register attempt %d (request_id=%s): selected node=%s", attempt, requestID, target)
		resp, err := c.registerOnEndpoint(target, requestID, record, registerAckTimeout)
		if err == nil {
			c.traceRegisterf("register attempt %d (request_id=%s): ack received from %s", attempt, requestID, target)
			return resp, nil
		}

		c.traceRegisterf("register attempt %d (request_id=%s): register failed on %s: %v", attempt, requestID, target, err)
		if !isRetryableRegisterError(err) {
			return nil, err
		}
		if isEmptyRegisterRejection(err) {
			c.traceRegisterf("register attempt %d (request_id=%s): empty RegisterService rejection received, retrying as transient condition", attempt, requestID)
		}
		c.traceRegisterf("register attempt %d (request_id=%s): no ack within %s (or transient error), restarting flow", attempt, requestID, registerAckTimeout)
		attempt++
		time.Sleep(registerRetryDelay)
	}
}

func (c *RegistryClient) traceRegisterf(format string, args ...any) {
	// Esegue la logica di trace registerf.
	if !c.registerTrace {
		return
	}
	if c.registerTraceOut == nil {
		c.registerTraceOut = os.Stderr
	}
	_, _ = fmt.Fprintf(c.registerTraceOut, format+"\n", args...)
}

func (c *RegistryClient) probeResponsiveEndpoints(timeout time.Duration) []string {
	// Esegue la logica di probe responsive endpoints.
	if timeout <= 0 {
		timeout = registerProbeTimeout
	}

	responsiveCh := make(chan string, len(c.endpoints))
	var wg sync.WaitGroup

	for _, endpoint := range c.endpoints {
		addr := endpoint
		wg.Add(1)
		go func() {
			defer wg.Done()
			if c.isEndpointResponsive(addr, timeout) {
				responsiveCh <- addr
			}
		}()
	}

	wg.Wait()
	close(responsiveCh)

	responsive := make([]string, 0, len(c.endpoints))
	for addr := range responsiveCh {
		responsive = append(responsive, addr)
	}
	return responsive
}

func (c *RegistryClient) isEndpointResponsive(address string, timeout time.Duration) bool {
	// Verifica la condizione richiesta.
	dialCtx, dialCancel := context.WithTimeout(context.Background(), timeout)
	conn, err := grpc.DialContext( //nolint:staticcheck // consistent with existing codebase
		dialCtx,
		address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	dialCancel()
	if err != nil {
		return false
	}
	defer conn.Close()

	callCtx, callCancel := context.WithTimeout(context.Background(), timeout)
	defer callCancel()

	stub := apiv1.NewServiceRegistryClient(conn)
	_, err = stub.Heartbeat(callCtx, &apiv1.HeartbeatRequest{
		ServiceName:   "__probe__",
		ServiceId:     "__probe__",
		HealthStatus:  apiv1.HealthStatus_HEALTH_STATUS_SERVING,
		HeartbeatUnix: time.Now().Unix(),
	})
	return err == nil
}

func (c *RegistryClient) registerOnEndpoint(address string, requestID string, record *apiv1.ServiceRecord, timeout time.Duration) (*apiv1.RegisterServiceResponse, error) {
	// Registra on endpoint.
	dialCtx, dialCancel := context.WithTimeout(context.Background(), timeout)
	conn, err := grpc.DialContext( //nolint:staticcheck // consistent with existing codebase
		dialCtx,
		address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	dialCancel()
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", address, err)
	}
	defer conn.Close()

	callCtx, callCancel := context.WithTimeout(context.Background(), timeout)
	defer callCancel()
	callCtx = metadata.NewOutgoingContext(callCtx, metadata.Pairs("x-request-id", requestID))

	stub := apiv1.NewServiceRegistryClient(conn)
	resp, err := stub.RegisterService(callCtx, &apiv1.RegisterServiceRequest{Record: record})
	if err != nil {
		return nil, fmt.Errorf("RegisterService RPC: %w", err)
	}
	if !resp.GetAccepted() {
		return nil, fmt.Errorf("RegisterService rejected: %s", resp.GetMessage())
	}
	return resp, nil
}

func newRequestID() string {
	// Crea un nuovo request id.
	b := make([]byte, 16)
	if _, err := crand.Read(b); err != nil {
		// Fallback keeps retries correlated even when crypto/rand is unavailable.
		return fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func isRetryableRegisterError(err error) bool {
	// Verifica la condizione richiesta.
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	msg := strings.ToLower(err.Error())
	if isEmptyRegisterRejection(err) {
		// Empty rejection reasons are treated as transient to allow retries.
		return true
	}
	return strings.Contains(msg, "context deadline exceeded") ||
		strings.Contains(msg, "rpc error: code = unavailable") ||
		strings.Contains(msg, "connection refused")
}

func isEmptyRegisterRejection(err error) bool {
	// Verifica la condizione richiesta.
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if !strings.HasPrefix(msg, "registerservice rejected:") {
		return false
	}
	reason := strings.TrimSpace(strings.TrimPrefix(msg, "registerservice rejected:"))
	return reason == ""
}

// Heartbeat sends a Heartbeat RPC for the given service instance.
func (c *RegistryClient) Heartbeat(serviceName, serviceID string, status apiv1.HealthStatus) error {
	// Gestisce il heartbeat del servizio.
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
	// Deregistra esegue la logica della funzione..
	if len(c.endpoints) == 0 {
		return fmt.Errorf("no registry endpoints configured")
	}

	requestID := newRequestID()
	for attempt := 1; ; attempt++ {
		order := rand.Perm(len(c.endpoints))
		var lastErr error
		for _, idx := range order {
			addr := c.endpoints[idx]
			err := c.deregisterOnEndpoint(addr, requestID, serviceName, serviceID, c.rpcTimeout)
			if err == nil {
				return nil
			}
			lastErr = err
			if !isRetryableRegisterError(err) {
				return err
			}
		}

		if lastErr == nil {
			return fmt.Errorf("deregister failed without error")
		}
		if attempt > 1 {
			return lastErr
		}
		time.Sleep(registerRetryDelay)
	}
}

func (c *RegistryClient) deregisterOnEndpoint(address string, requestID string, serviceName, serviceID string, timeout time.Duration) error {
	// Deregistra on endpoint.
	dialCtx, dialCancel := context.WithTimeout(context.Background(), timeout)
	conn, err := grpc.DialContext( //nolint:staticcheck // consistent with existing codebase
		dialCtx,
		address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	dialCancel()
	if err != nil {
		return fmt.Errorf("dial %s: %w", address, err)
	}
	defer conn.Close()

	callCtx, callCancel := context.WithTimeout(context.Background(), timeout)
	defer callCancel()
	callCtx = metadata.NewOutgoingContext(callCtx, metadata.Pairs("x-request-id", requestID))

	stub := apiv1.NewServiceRegistryClient(conn)
	resp, err := stub.DeregisterService(callCtx, &apiv1.DeregisterServiceRequest{
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
}

// Get returns the registry records for the given service name and optional id.
func (c *RegistryClient) Get(serviceName, serviceID string) ([]*apiv1.ServiceRecord, error) {
	// Recupera esegue la logica della funzione..
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
	// Elenca esegue la logica della funzione..
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
