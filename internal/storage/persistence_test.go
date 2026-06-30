package storage

import (
	"os"
	"path/filepath"
	"testing"

	apiv1 "github.com/leonardomonnati2796/distributed-service-registry/pkg/api"
)

func TestSnapshotRoundTrip(t *testing.T) {
	dir := t.TempDir()
	services := []*apiv1.ServiceRecord{{
		ServiceName:       "users",
		ServiceId:         "u1",
		Endpoint:          "users-1:8080",
		Version:           "v1.0.0",
		HealthStatus:      apiv1.HealthStatus_HEALTH_STATUS_SERVING,
		LastHeartbeatUnix: 100,
		UpdatedAtUnix:     100,
		OwnerNodeId:       "node-a",
		LogicalVersion:    2,
	}}
	peers := []*apiv1.NodeInfo{{NodeId: "node-a", GrpcAddress: "node-a:50051", UpdatedAtUnix: 100}}

	if err := SaveSnapshot(dir, services, peers); err != nil {
		t.Fatalf("save snapshot failed: %v", err)
	}

	snapshot, err := LoadSnapshot(dir)
	if err != nil {
		t.Fatalf("load snapshot failed: %v", err)
	}
	if len(snapshot.Services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(snapshot.Services))
	}
	if len(snapshot.Peers) != 1 {
		t.Fatalf("expected 1 peer, got %d", len(snapshot.Peers))
	}
	if snapshot.Services[0].GetServiceName() != "users" || snapshot.Peers[0].GetNodeId() != "node-a" {
		t.Fatalf("unexpected snapshot content after roundtrip")
	}
}

func TestLoadSnapshotMissingReturnsEmpty(t *testing.T) {
	snapshot, err := LoadSnapshot(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatalf("expected no error for missing snapshot, got %v", err)
	}
	if len(snapshot.Services) != 0 || len(snapshot.Peers) != 0 {
		t.Fatalf("expected empty snapshot for missing file")
	}
}

func TestLoadSnapshotCorruptReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, snapshotFileName)
	if err := os.WriteFile(path, []byte("not-json"), 0o644); err != nil {
		t.Fatalf("write corrupt snapshot failed: %v", err)
	}

	_, err := LoadSnapshot(dir)
	if err == nil {
		t.Fatalf("expected error for corrupt snapshot")
	}
}

