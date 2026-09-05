//go:build integration
// +build integration

package test

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/leonardomonnati2796/distributed-service-registry/internal/config"
	"github.com/leonardomonnati2796/distributed-service-registry/internal/gossip"
	"github.com/leonardomonnati2796/distributed-service-registry/internal/registry"
	"github.com/leonardomonnati2796/distributed-service-registry/internal/storage"
	apiv1 "github.com/leonardomonnati2796/distributed-service-registry/pkg/api"
)

type testNode struct {
	id          string
	address     string
	service     *storage.ServiceStore
	peers       *storage.PeerStore
	gossip      *gossip.Runtime
	grpcServer  *grpc.Server
	listener    net.Listener
	stopOnce    sync.Once
	serveErrCh  chan error
	serveStopCh chan struct{}
}

func startTestNode(t *testing.T, id string, seedPeers []string) *testNode {
	// Avvia l'esecuzione del componente.
	return startTestNodeWithPersistenceAndPeerTimeout(t, id, seedPeers, "", 2)
}

func startTestNodeWithPersistence(t *testing.T, id string, seedPeers []string, storageDir string) *testNode {
	// Avvia l'esecuzione del componente.
	return startTestNodeWithPersistenceAndPeerTimeout(t, id, seedPeers, storageDir, 2)
}

func startTestNodeWithPersistenceAndPeerTimeout(t *testing.T, id string, seedPeers []string, storageDir string, peerTimeoutSeconds int) *testNode {
	// Avvia l'esecuzione del componente.
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}

	address := listener.Addr().String()
	cfg := &config.RegistryConfig{
		Node: config.RegistryNodeConfig{
			ID:               id,
			ListenAddress:    address,
			AdvertiseAddress: address,
		},
		Cluster: config.RegistryClusterConfig{
			SeedPeers:                append([]string(nil), seedPeers...),
			GossipIntervalSeconds:    1,
			ReconcileIntervalSeconds: 1,
			PeerTimeoutSeconds:       peerTimeoutSeconds,
			MaxGossipFanout:          2,
		},
	}

	serviceStore := storage.NewServiceStore()
	peerStore := storage.NewPeerStore()
	_ = storageDir
	peerStore.UpsertSelf(id, address, time.Now().Unix())

	grpcServer := grpc.NewServer()
	serviceServer := registry.NewServiceRegistryServer(serviceStore, id)
	peerServer := registry.NewRegistryPeerServer(serviceStore, peerStore, id, address)
	apiv1.RegisterServiceRegistryServer(grpcServer, serviceServer)
	apiv1.RegisterRegistryPeerServer(grpcServer, peerServer)
	registry.RegisterRegistryPeerControlServer(grpcServer, peerServer)

	node := &testNode{
		id:          id,
		address:     address,
		service:     serviceStore,
		peers:       peerStore,
		grpcServer:  grpcServer,
		listener:    listener,
		serveErrCh:  make(chan error, 1),
		serveStopCh: make(chan struct{}),
	}

	go func() {
		if serveErr := grpcServer.Serve(listener); serveErr != nil {
			select {
			case <-node.serveStopCh:
				return
			default:
			}
			node.serveErrCh <- serveErr
		}
	}()

	node.gossip = gossip.NewRuntime(cfg, serviceStore, peerStore)
	node.gossip.Start()

	return node
}

func (n *testNode) Stop() {
	// Arresta l'esecuzione corrente.
	n.stopOnce.Do(func() {
		close(n.serveStopCh)
		if n.gossip != nil {
			n.gossip.Stop()
		}
		if n.grpcServer != nil {
			n.grpcServer.GracefulStop()
		}
		if n.listener != nil {
			_ = n.listener.Close()
		}
	})
}

func TestMultiNodeGossipConvergenceAndPeerPruning(t *testing.T) {
	// Esegue il test per multi node gossip convergence and peer pruning.
	nodeA := startTestNode(t, "node-a", nil)
	defer nodeA.Stop()

	nodeB := startTestNode(t, "node-b", []string{nodeA.address})
	defer nodeB.Stop()

	nodeC := startTestNode(t, "node-c", []string{nodeA.address})
	defer nodeC.Stop()

	waitFor(t, 8*time.Second, "node-a sees both other peers", func() bool {
		peers := nodeA.peers.List()
		return hasPeer(peers, "node-b") && hasPeer(peers, "node-c")
	})

	err := registerService(nodeA.address, &apiv1.ServiceRecord{
		ServiceName:  "users",
		Endpoint:     "users-1:8080",
		HealthStatus: apiv1.HealthStatus_HEALTH_STATUS_SERVING,
	})
	if err != nil {
		t.Fatalf("register service on node-a failed: %v", err)
	}

	waitFor(t, 8*time.Second, "service converges to node-b", func() bool {
		return hasService(nodeB.service.List(), "users")
	})
	waitFor(t, 8*time.Second, "service converges to node-c", func() bool {
		return hasService(nodeC.service.List(), "users")
	})

	nodeC.Stop()

	waitFor(t, 10*time.Second, "node-c pruned as stale by node-a", func() bool {
		peers := nodeA.peers.List()
		return hasPeer(peers, "node-b") && !hasPeer(peers, "node-c")
	})
}

func TestCrashResumeAndStateRealignment(t *testing.T) {
	// Esegue il test per crash resume and state realignment.
	nodeA := startTestNode(t, "node-a", nil)
	defer nodeA.Stop()

	nodeC := startTestNode(t, "node-c", []string{nodeA.address})
	defer nodeC.Stop()

	nodeB := startTestNodeWithPersistence(t, "node-b", []string{nodeA.address}, "")

	waitFor(t, 8*time.Second, "node-a sees node-b and node-c", func() bool {
		peers := nodeA.peers.List()
		return hasPeer(peers, "node-b") && hasPeer(peers, "node-c")
	})

	err := registerService(nodeB.address, &apiv1.ServiceRecord{
		ServiceName:  "billing",
		Endpoint:     "billing-1:8080",
		HealthStatus: apiv1.HealthStatus_HEALTH_STATUS_SERVING,
	})
	if err != nil {
		t.Fatalf("register service on node-b failed: %v", err)
	}

	waitFor(t, 8*time.Second, "billing service converges to node-a", func() bool {
		return hasService(nodeA.service.List(), "billing")
	})

	nodeB.Stop()

	waitFor(t, 10*time.Second, "node-b pruned from node-a after crash", func() bool {
		return !hasPeer(nodeA.peers.List(), "node-b")
	})

	err = registerService(nodeA.address, &apiv1.ServiceRecord{
		ServiceName:  "orders",
		Endpoint:     "orders-1:8080",
		HealthStatus: apiv1.HealthStatus_HEALTH_STATUS_SERVING,
	})
	if err != nil {
		t.Fatalf("register service on node-a failed: %v", err)
	}

	nodeBResumed := startTestNodeWithPersistence(t, "node-b", []string{nodeA.address}, "")
	defer nodeBResumed.Stop()

	waitFor(t, 8*time.Second, "node-a sees resumed node-b", func() bool {
		return hasPeer(nodeA.peers.List(), "node-b")
	})

	waitFor(t, 8*time.Second, "resumed node-b recovered billing service from peers", func() bool {
		return hasService(nodeBResumed.service.List(), "billing")
	})

	waitFor(t, 8*time.Second, "resumed node-b realigns orders service from cluster", func() bool {
		return hasService(nodeBResumed.service.List(), "orders")
	})
}

func TestDeregisterConvergesAcrossNodes(t *testing.T) {
	// Esegue il test per deregister converges across nodes.
	nodeA := startTestNode(t, "node-a", nil)
	defer nodeA.Stop()

	nodeB := startTestNode(t, "node-b", []string{nodeA.address})
	defer nodeB.Stop()

	err := registerService(nodeA.address, &apiv1.ServiceRecord{
		ServiceName:  "catalog",
		Endpoint:     "catalog-1:8080",
		HealthStatus: apiv1.HealthStatus_HEALTH_STATUS_SERVING,
	})
	if err != nil {
		t.Fatalf("register service on node-a failed: %v", err)
	}

	waitFor(t, 8*time.Second, "service converges to node-b", func() bool {
		return hasService(nodeB.service.List(), "catalog")
	})

	err = deregisterService(nodeA.address, "catalog")
	if err != nil {
		t.Fatalf("deregister service on node-a failed: %v", err)
	}

	waitFor(t, 8*time.Second, "deregister converges to node-b", func() bool {
		return !hasService(nodeB.service.List(), "catalog")
	})
}

func TestGracefulLeaveConvergesWithoutPeerTimeout(t *testing.T) {
	// Esegue il test per graceful leave converges without peer timeout.
	nodeA := startTestNodeWithPersistenceAndPeerTimeout(t, "node-a", nil, "", 30)
	defer nodeA.Stop()

	nodeB := startTestNodeWithPersistenceAndPeerTimeout(t, "node-b", []string{nodeA.address}, "", 30)
	defer nodeB.Stop()

	waitFor(t, 5*time.Second, "node-a sees node-b", func() bool {
		return hasPeer(nodeA.peers.List(), "node-b")
	})

	nodeB.gossip.Stop()
	nodeB.gossip.GracefulLeave()

	waitFor(t, 3*time.Second, "node-b removed by explicit leave", func() bool {
		return !hasPeer(nodeA.peers.List(), "node-b")
	})
}

func registerService(address string, record *apiv1.ServiceRecord) error {
	// Registra service.
	return withServiceClient(address, func(client apiv1.ServiceRegistryClient) error {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		resp, err := client.RegisterService(ctx, &apiv1.RegisterServiceRequest{Record: record})
		if err != nil {
			return err
		}
		if !resp.GetAccepted() {
			return fmt.Errorf("register rejected: %s", resp.GetMessage())
		}
		return nil
	})
}

func deregisterService(address, serviceName string) error {
	// Deregistra service.
	return withServiceClient(address, func(client apiv1.ServiceRegistryClient) error {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		resp, err := client.DeregisterService(ctx, &apiv1.DeregisterServiceRequest{
			ServiceName: serviceName,
		})
		if err != nil {
			return err
		}
		if !resp.GetAccepted() {
			return fmt.Errorf("deregister rejected: %s", resp.GetMessage())
		}
		return nil
	})
}

func withServiceClient(address string, fn func(client apiv1.ServiceRegistryClient) error) error {
	// Esegue la logica di with service client.
	dialCtx, dialCancel := context.WithTimeout(context.Background(), 3*time.Second)
	conn, err := grpc.DialContext(dialCtx, address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	dialCancel()
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	return fn(apiv1.NewServiceRegistryClient(conn))
}

func waitFor(t *testing.T, timeout time.Duration, message string, condition func() bool) {
	// Attende il completamento della condizione richiesta.
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for condition: %s", message)
}

func hasPeer(peers []*apiv1.NodeInfo, nodeID string) bool {
	// Controlla la presenza del valore richiesto.
	for _, peer := range peers {
		if peer.GetNodeId() == nodeID {
			return true
		}
	}
	return false
}

func hasService(records []*apiv1.ServiceRecord, name string) bool {
	// Controlla la presenza del valore richiesto.
	for _, record := range records {
		if record.GetServiceName() == name {
			return true
		}
	}
	return false
}
