package storage

import (
	"testing"

	apiv1 "github.com/leonardomonnati2796/distributed-service-registry/pkg/api"
)

func TestPeerStoreUpsertAndMerge(t *testing.T) {
	store := NewPeerStore()

	store.UpsertSelf("node-a", "node-a:50051", 10)

	if changed := store.Upsert(&apiv1.NodeInfo{NodeId: "node-a", GrpcAddress: "node-a:60000", UpdatedAtUnix: 9}); changed {
		t.Fatalf("expected older peer update to be ignored")
	}
	if changed := store.Upsert(&apiv1.NodeInfo{NodeId: "node-a", GrpcAddress: "node-a:60000", UpdatedAtUnix: 12}); !changed {
		t.Fatalf("expected newer peer update to be accepted")
	}

	merged := store.MergeRemote([]*apiv1.NodeInfo{{NodeId: "node-b", GrpcAddress: "node-b:50051", UpdatedAtUnix: 11}})
	if merged != 1 {
		t.Fatalf("expected one merged peer, got %d", merged)
	}

	peers := store.List()
	if len(peers) != 2 {
		t.Fatalf("expected two peers, got %d", len(peers))
	}
	if peers[0].GetNodeId() != "node-a" || peers[0].GetGrpcAddress() != "node-a:60000" {
		t.Fatalf("expected node-a address to be updated")
	}
}

func TestPeerStoreRandomAndPrune(t *testing.T) {
	store := NewPeerStore()
	store.UpsertSelf("node-self", "self:50051", 100)
	store.Upsert(&apiv1.NodeInfo{NodeId: "node-1", GrpcAddress: "node-1:50051", UpdatedAtUnix: 50})
	store.Upsert(&apiv1.NodeInfo{NodeId: "node-2", GrpcAddress: "node-2:50051", UpdatedAtUnix: 95})

	random := store.RandomPeers(2, map[string]struct{}{"node-self": {}})
	if len(random) == 0 {
		t.Fatalf("expected at least one random peer")
	}
	for _, peer := range random {
		if peer.GetNodeId() == "node-self" {
			t.Fatalf("self node must be excluded from random peers")
		}
	}

	removed := store.RemoveStale(100, 10, map[string]struct{}{"node-self": {}})
	if removed != 1 {
		t.Fatalf("expected one stale peer removed, got %d", removed)
	}

	remaining := store.List()
	if len(remaining) != 2 {
		t.Fatalf("expected two peers after prune (self + node-2), got %d", len(remaining))
	}
}

