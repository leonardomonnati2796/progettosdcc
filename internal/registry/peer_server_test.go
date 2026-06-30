package registry

import (
	"context"
	"testing"
	"time"

	"github.com/leonardomonnati2796/distributed-service-registry/internal/storage"
	apiv1 "github.com/leonardomonnati2796/distributed-service-registry/pkg/api"
)

func TestRegistryPeerServerJoinAndGossipMerge(t *testing.T) {
	serviceStore := storage.NewServiceStore()
	peerStore := storage.NewPeerStore()
	server := NewRegistryPeerServer(serviceStore, peerStore, "node-a", "node-a:50051")

	now := int64(100)
	server.now = func() time.Time { return time.Unix(now, 0) }

	joinResp, err := server.JoinCluster(context.Background(), &apiv1.JoinClusterRequest{
		Node: &apiv1.NodeInfo{NodeId: "node-b", GrpcAddress: "node-b:50051"},
	})
	if err != nil {
		t.Fatalf("join returned error: %v", err)
	}
	if len(joinResp.GetPeers()) != 2 {
		t.Fatalf("expected 2 peers after join, got %d", len(joinResp.GetPeers()))
	}

	now = 101
	gossipResp, err := server.GossipSync(context.Background(), &apiv1.GossipSyncRequest{
		SourceNodeId: "node-b",
		Records: []*apiv1.ServiceRecord{
			{
				ServiceName:       "users",
				ServiceId:         "u1",
				Endpoint:          "users-1:8080",
				Version:           "v1",
				HealthStatus:      apiv1.HealthStatus_HEALTH_STATUS_SERVING,
				LastHeartbeatUnix: 101,
				UpdatedAtUnix:     101,
				OwnerNodeId:       "node-b",
				LogicalVersion:    1,
			},
		},
		Peers: []*apiv1.NodeInfo{
			{NodeId: "node-b", GrpcAddress: "node-b:50051", UpdatedAtUnix: 101},
		},
		SentAtUnix: 101,
	})
	if err != nil {
		t.Fatalf("gossip returned error: %v", err)
	}
	if !gossipResp.GetAccepted() {
		t.Fatalf("expected gossip accepted=true")
	}

	pullResp, err := server.PullState(context.Background(), &apiv1.PullStateRequest{SourceNodeId: "node-b", SinceUnix: 0})
	if err != nil {
		t.Fatalf("pull state returned error: %v", err)
	}
	if len(pullResp.GetRecords()) != 1 {
		t.Fatalf("expected 1 service record after gossip merge, got %d", len(pullResp.GetRecords()))
	}
	if pullResp.GetRecords()[0].GetServiceName() != "users" {
		t.Fatalf("expected merged service record for users")
	}
	if len(pullResp.GetPeers()) < 2 {
		t.Fatalf("expected at least 2 peers in pull response")
	}
}

func TestRegistryPeerServerLeaveCluster(t *testing.T) {
	serviceStore := storage.NewServiceStore()
	peerStore := storage.NewPeerStore()
	server := NewRegistryPeerServer(serviceStore, peerStore, "node-a", "node-a:50051")

	server.now = func() time.Time { return time.Unix(200, 0) }
	peerStore.UpsertSelf("node-a", "node-a:50051", 200)
	peerStore.Upsert(&apiv1.NodeInfo{NodeId: "node-b", GrpcAddress: "node-b:50051", UpdatedAtUnix: 200})

	resp, err := server.LeaveCluster(context.Background(), &apiv1.JoinClusterRequest{
		Node: &apiv1.NodeInfo{NodeId: "node-b", GrpcAddress: "node-b:50051", UpdatedAtUnix: 201},
	})
	if err != nil {
		t.Fatalf("leave cluster returned error: %v", err)
	}
	if !resp.GetAccepted() {
		t.Fatalf("expected leave accepted=true")
	}

	peers := peerStore.List()
	if len(peers) != 1 {
		t.Fatalf("expected one peer after leave, got %d", len(peers))
	}
	if peers[0].GetNodeId() != "node-a" {
		t.Fatalf("expected node-b to be removed from peer store")
	}
}

