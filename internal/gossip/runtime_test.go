package gossip

import (
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"

	"github.com/leonardomonnati2796/distributed-service-registry/internal/config"
	"github.com/leonardomonnati2796/distributed-service-registry/internal/registry"
	"github.com/leonardomonnati2796/distributed-service-registry/internal/storage"
	apiv1 "github.com/leonardomonnati2796/distributed-service-registry/pkg/api"
)

func TestNewRuntimeAppliesDefaults(t *testing.T) {
	cfg := &config.RegistryConfig{
		Node: config.RegistryNodeConfig{
			ID:               "node-a",
			AdvertiseAddress: "node-a:50051",
		},
		Cluster: config.RegistryClusterConfig{},
	}

	r := NewRuntime(cfg, storage.NewServiceStore(), storage.NewPeerStore())

	if r.gossipInterval != 3*time.Second {
		t.Fatalf("expected default gossip interval 3s, got %s", r.gossipInterval)
	}
	if r.reconcileInterval != 15*time.Second {
		t.Fatalf("expected default reconcile interval 15s, got %s", r.reconcileInterval)
	}
	if r.peerTimeout != 10*time.Second {
		t.Fatalf("expected default peer timeout 10s, got %s", r.peerTimeout)
	}
	if r.maxGossipFanout != 2 {
		t.Fatalf("expected default max fanout 2, got %d", r.maxGossipFanout)
	}
}

func TestRuntimeBootstrapGossipAndReconcile(t *testing.T) {
	remoteAddress, remoteServiceStore, _, stopRemote := startPeerServer(t, "node-remote")
	defer stopRemote()

	nowUnix := time.Now().Unix()
	remoteServiceStore.Upsert(&apiv1.ServiceRecord{
		ServiceName:       "remote-users",
		ServiceId:         "r1",
		Endpoint:          "remote-users:8080",
		Version:           "v1.0.0",
		HealthStatus:      apiv1.HealthStatus_HEALTH_STATUS_SERVING,
		LastHeartbeatUnix: nowUnix,
		UpdatedAtUnix:     nowUnix,
		OwnerNodeId:       "node-remote",
		LogicalVersion:    1,
	})

	localServices := storage.NewServiceStore()
	localPeers := storage.NewPeerStore()
	cfg := &config.RegistryConfig{
		Node: config.RegistryNodeConfig{
			ID:               "node-local",
			AdvertiseAddress: "node-local:50051",
		},
		Cluster: config.RegistryClusterConfig{
			SeedPeers:                []string{remoteAddress},
			GossipIntervalSeconds:    1,
			ReconcileIntervalSeconds: 1,
			PeerTimeoutSeconds:       2,
			MaxGossipFanout:          1,
		},
	}

	runtime := NewRuntime(cfg, localServices, localPeers)
	localPeers.UpsertSelf("node-local", "node-local:50051", nowUnix)

	runtime.bootstrapFromSeeds()
	if !containsService(localServices.List(), "remote-users", "r1") {
		t.Fatalf("expected bootstrap to merge remote service")
	}

	localServices.Upsert(&apiv1.ServiceRecord{
		ServiceName:       "local-orders",
		ServiceId:         "l1",
		Endpoint:          "local-orders:8080",
		Version:           "v1.0.0",
		HealthStatus:      apiv1.HealthStatus_HEALTH_STATUS_SERVING,
		LastHeartbeatUnix: nowUnix,
		UpdatedAtUnix:     nowUnix,
		OwnerNodeId:       "node-local",
		LogicalVersion:    1,
	})

	runtime.runGossipRound()
	if !containsService(remoteServiceStore.List(), "local-orders", "l1") {
		t.Fatalf("expected gossip round to push local service to remote node")
	}

	remoteServiceStore.Upsert(&apiv1.ServiceRecord{
		ServiceName:       "remote-payments",
		ServiceId:         "r2",
		Endpoint:          "remote-payments:8080",
		Version:           "v1.0.0",
		HealthStatus:      apiv1.HealthStatus_HEALTH_STATUS_SERVING,
		LastHeartbeatUnix: nowUnix + 2,
		UpdatedAtUnix:     nowUnix + 2,
		OwnerNodeId:       "node-remote",
		LogicalVersion:    1,
	})

	runtime.lastReconcileUnix.Store(nowUnix - 1)
	runtime.runReconcileRound()
	if !containsService(localServices.List(), "remote-payments", "r2") {
		t.Fatalf("expected reconcile round to pull remote incremental updates")
	}
}

func TestRuntimeGracefulLeaveRemovesLocalPeer(t *testing.T) {
	remoteAddress, _, remotePeerStore, stopRemote := startPeerServer(t, "node-remote")
	defer stopRemote()

	localServices := storage.NewServiceStore()
	localPeers := storage.NewPeerStore()
	cfg := &config.RegistryConfig{
		Node: config.RegistryNodeConfig{
			ID:               "node-local",
			AdvertiseAddress: "node-local:50051",
		},
		Cluster: config.RegistryClusterConfig{
			SeedPeers:                []string{remoteAddress},
			GossipIntervalSeconds:    1,
			ReconcileIntervalSeconds: 1,
			PeerTimeoutSeconds:       2,
			MaxGossipFanout:          1,
		},
	}

	runtime := NewRuntime(cfg, localServices, localPeers)
	runtime.Start()
	t.Cleanup(runtime.Stop)

	waitForCondition(t, 3*time.Second, func() bool {
		return containsPeer(remotePeerStore.List(), "node-local")
	})

	runtime.GracefulLeave()

	waitForCondition(t, 3*time.Second, func() bool {
		return !containsPeer(remotePeerStore.List(), "node-local")
	})
}

func startPeerServer(t *testing.T, nodeID string) (address string, serviceStore *storage.ServiceStore, peerStore *storage.PeerStore, stop func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	address = listener.Addr().String()

	serviceStore = storage.NewServiceStore()
	peerStore = storage.NewPeerStore()
	peerStore.UpsertSelf(nodeID, address, time.Now().Unix())

	grpcServer := grpc.NewServer()
	peerServer := registry.NewRegistryPeerServer(serviceStore, peerStore, nodeID, address)
	apiv1.RegisterRegistryPeerServer(grpcServer, peerServer)
	registry.RegisterRegistryPeerControlServer(grpcServer, peerServer)

	go func() {
		_ = grpcServer.Serve(listener)
	}()

	stop = func() {
		grpcServer.GracefulStop()
		_ = listener.Close()
	}
	return address, serviceStore, peerStore, stop
}

func waitForCondition(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for condition")
}

func containsService(records []*apiv1.ServiceRecord, name, id string) bool {
	for _, record := range records {
		if record.GetServiceName() == name && record.GetServiceId() == id {
			return true
		}
	}
	return false
}

func containsPeer(peers []*apiv1.NodeInfo, nodeID string) bool {
	for _, peer := range peers {
		if peer.GetNodeId() == nodeID {
			return true
		}
	}
	return false
}

