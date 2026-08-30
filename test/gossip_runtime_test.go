package test

import (
	"context"
	"net"
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

func TestNewRuntimeCreatesWorkingRuntime(t *testing.T) {
// Esegue il test per new runtime creates working runtime.
	cfg := &config.RegistryConfig{
		Node: config.RegistryNodeConfig{
			ID:               "node-a",
			AdvertiseAddress: "node-a:50051",
		},
		Cluster: config.RegistryClusterConfig{},
	}

	r := gossip.NewRuntime(cfg, storage.NewServiceStore(), storage.NewPeerStore())
	if r == nil {
		t.Fatal("expected runtime instance")
	}
	if r == nil {
		t.Fatal("runtime should not be nil")
	}
	if r != nil {
		r.Start()
		t.Cleanup(r.Stop)
	}
}

func TestRuntimeBootstrapGossipAndReconcile(t *testing.T) {
// Esegue il test per runtime bootstrap gossip and reconcile.
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

	_ = gossip.NewRuntime(cfg, localServices, localPeers)
	localPeers.UpsertSelf("node-local", "node-local:50051", nowUnix)

	waitForCondition(t, 3*time.Second, func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		conn, err := grpc.DialContext(ctx, remoteAddress, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	})
	localPeers.Upsert(&apiv1.NodeInfo{NodeId: "node-remote", GrpcAddress: remoteAddress, UpdatedAtUnix: nowUnix})

	localServices.Upsert(&apiv1.ServiceRecord{
		ServiceName:       "local-orders",
		ServiceId:         "l1",
		Endpoint:          "local-orders:8080",
		Version:           "v1.0.0",
		HealthStatus:      apiv1.HealthStatus_HEALTH_STATUS_SERVING,
		LastHeartbeatUnix: nowUnix,
		OwnerNodeId:       "node-local",
		LogicalVersion:    1,
	})

	if merged := remoteServiceStore.MergeRemote(localServices.ListForSync()); merged != 1 {
		t.Fatalf("expected direct gossip merge to push one local service to remote node, got %d", merged)
	}
	if !containsService(remoteServiceStore.List(), "local-orders", "l1") {
		t.Fatalf("expected gossip merge to push local service to remote node")
	}

	remoteServiceStore.Upsert(&apiv1.ServiceRecord{
		ServiceName:       "remote-payments",
		ServiceId:         "r2",
		Endpoint:          "remote-payments:8080",
		Version:           "v1.0.0",
		HealthStatus:      apiv1.HealthStatus_HEALTH_STATUS_SERVING,
		LastHeartbeatUnix: nowUnix + 2,
		OwnerNodeId:       "node-remote",
		LogicalVersion:    1,
	})

	if merged := localServices.MergeRemote(remoteServiceStore.ListForSync()); merged != 2 {
		t.Fatalf("expected direct reconcile merge to pull two remote services, got %d", merged)
	}
	if !containsService(localServices.List(), "remote-users", "r1") || !containsService(localServices.List(), "remote-payments", "r2") {
		t.Fatalf("expected reconcile merge to pull remote updates")
	}
}

func TestRuntimeGracefulLeaveRemovesLocalPeer(t *testing.T) {
// Esegue il test per runtime graceful leave removes local peer.
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

	runtime := gossip.NewRuntime(cfg, localServices, localPeers)
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
// Avvia l'esecuzione del componente.
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
// Attende il completamento della condizione richiesta.
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
// Controlla se il contenuto richiesto ? presente.
	for _, record := range records {
		if record.GetServiceName() == name && record.GetServiceId() == id {
			return true
		}
	}
	return false
}

func containsPeer(peers []*apiv1.NodeInfo, nodeID string) bool {
// Controlla se il contenuto richiesto ? presente.
	for _, peer := range peers {
		if peer.GetNodeId() == nodeID {
			return true
		}
	}
	return false
}
