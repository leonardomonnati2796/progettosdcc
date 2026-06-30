package storage

import (
	"testing"

	apiv1 "github.com/leonardomonnati2796/distributed-service-registry/pkg/api"
)

func TestServiceStoreUpsertListAndVersioning(t *testing.T) {
	store := NewServiceStore()

	first := &apiv1.ServiceRecord{
		ServiceName:       "users",
		ServiceId:         "b",
		Endpoint:          "users-b:8080",
		Version:           "v1",
		HealthStatus:      apiv1.HealthStatus_HEALTH_STATUS_SERVING,
		LastHeartbeatUnix: 100,
		UpdatedAtUnix:     100,
	}
	storedFirst := store.Upsert(first)
	if storedFirst.GetLogicalVersion() != 1 {
		t.Fatalf("expected logical version 1, got %d", storedFirst.GetLogicalVersion())
	}

	second := &apiv1.ServiceRecord{
		ServiceName:       "users",
		ServiceId:         "a",
		Endpoint:          "users-a:8080",
		Version:           "v1",
		HealthStatus:      apiv1.HealthStatus_HEALTH_STATUS_SERVING,
		LastHeartbeatUnix: 100,
		UpdatedAtUnix:     100,
	}
	store.Upsert(second)

	updatedFirst := &apiv1.ServiceRecord{
		ServiceName:       "users",
		ServiceId:         "b",
		Endpoint:          "users-b:9090",
		Version:           "v2",
		HealthStatus:      apiv1.HealthStatus_HEALTH_STATUS_DEGRADED,
		LastHeartbeatUnix: 101,
		UpdatedAtUnix:     101,
	}
	storedUpdated := store.Upsert(updatedFirst)
	if storedUpdated.GetLogicalVersion() != 2 {
		t.Fatalf("expected logical version 2 after update, got %d", storedUpdated.GetLogicalVersion())
	}

	all := store.List()
	if len(all) != 2 {
		t.Fatalf("expected 2 records, got %d", len(all))
	}
	if all[0].GetServiceId() != "a" || all[1].GetServiceId() != "b" {
		t.Fatalf("expected deterministic ordering by service id, got %s then %s", all[0].GetServiceId(), all[1].GetServiceId())
	}
	if all[1].GetEndpoint() != "users-b:9090" {
		t.Fatalf("expected endpoint update to persist, got %s", all[1].GetEndpoint())
	}
}

func TestServiceStoreHeartbeatStaleAndRemove(t *testing.T) {
	store := NewServiceStore()
	store.Upsert(&apiv1.ServiceRecord{
		ServiceName:       "orders",
		ServiceId:         "1",
		Endpoint:          "orders-1:8080",
		Version:           "v1",
		HealthStatus:      apiv1.HealthStatus_HEALTH_STATUS_SERVING,
		LastHeartbeatUnix: 200,
		UpdatedAtUnix:     200,
	})

	updated, ok := store.UpdateHeartbeat("orders", "1", apiv1.HealthStatus_HEALTH_STATUS_DEGRADED, 205, 205)
	if !ok {
		t.Fatalf("expected heartbeat update to succeed")
	}
	if updated.GetHealthStatus() != apiv1.HealthStatus_HEALTH_STATUS_DEGRADED {
		t.Fatalf("expected health to be DEGRADED, got %s", updated.GetHealthStatus().String())
	}

	staleCount := store.MarkStale(220, 10)
	if staleCount != 1 {
		t.Fatalf("expected one stale update, got %d", staleCount)
	}
	postStale := store.Get("orders", "1")
	if len(postStale) != 1 {
		t.Fatalf("expected to find one record after stale update")
	}
	if postStale[0].GetHealthStatus() != apiv1.HealthStatus_HEALTH_STATUS_NOT_SERVING {
		t.Fatalf("expected health to become NOT_SERVING, got %s", postStale[0].GetHealthStatus().String())
	}

	if removed := store.Remove("orders", "1", 221); !removed {
		t.Fatalf("expected remove to return true")
	}
	if removedAgain := store.Remove("orders", "1", 222); removedAgain {
		t.Fatalf("expected second remove to return false")
	}
}

func TestServiceStoreTombstonePreventsResurrection(t *testing.T) {
	store := NewServiceStore()
	store.Upsert(&apiv1.ServiceRecord{
		ServiceName:       "users",
		ServiceId:         "u1",
		Endpoint:          "users-1:8080",
		Version:           "v1",
		HealthStatus:      apiv1.HealthStatus_HEALTH_STATUS_SERVING,
		LastHeartbeatUnix: 100,
		UpdatedAtUnix:     100,
		OwnerNodeId:       "node-a",
		LogicalVersion:    1,
	})

	if removed := store.Remove("users", "u1", 101); !removed {
		t.Fatalf("expected remove to create tombstone")
	}

	if got := store.Get("users", "u1"); len(got) != 0 {
		t.Fatalf("expected API query to hide tombstoned record")
	}

	merged := store.MergeRemote([]*apiv1.ServiceRecord{
		{
			ServiceName:       "users",
			ServiceId:         "u1",
			Endpoint:          "users-old:8080",
			Version:           "v1",
			HealthStatus:      apiv1.HealthStatus_HEALTH_STATUS_SERVING,
			LastHeartbeatUnix: 100,
			UpdatedAtUnix:     100,
			OwnerNodeId:       "node-b",
			LogicalVersion:    1,
		},
	})
	if merged != 0 {
		t.Fatalf("expected stale remote record to be ignored after tombstone")
	}
}

func TestServiceStoreMergeRemoteConflictResolution(t *testing.T) {
	store := NewServiceStore()
	store.Upsert(&apiv1.ServiceRecord{
		ServiceName:       "payments",
		ServiceId:         "p1",
		Endpoint:          "payments-local:8080",
		Version:           "v1",
		HealthStatus:      apiv1.HealthStatus_HEALTH_STATUS_SERVING,
		LastHeartbeatUnix: 100,
		UpdatedAtUnix:     100,
		OwnerNodeId:       "node-a",
		LogicalVersion:    1,
	})

	merged := store.MergeRemote([]*apiv1.ServiceRecord{
		{
			ServiceName:       "payments",
			ServiceId:         "p1",
			Endpoint:          "payments-remote:8080",
			Version:           "v2",
			HealthStatus:      apiv1.HealthStatus_HEALTH_STATUS_DEGRADED,
			LastHeartbeatUnix: 101,
			UpdatedAtUnix:     101,
			OwnerNodeId:       "node-b",
			LogicalVersion:    2,
		},
	})
	if merged != 1 {
		t.Fatalf("expected one merged record, got %d", merged)
	}

	afterMerge := store.Get("payments", "p1")
	if len(afterMerge) != 1 || afterMerge[0].GetEndpoint() != "payments-remote:8080" {
		t.Fatalf("expected remote endpoint to win on higher version")
	}

	mergedOlder := store.MergeRemote([]*apiv1.ServiceRecord{
		{
			ServiceName:       "payments",
			ServiceId:         "p1",
			Endpoint:          "payments-old:8080",
			Version:           "v0",
			HealthStatus:      apiv1.HealthStatus_HEALTH_STATUS_NOT_SERVING,
			LastHeartbeatUnix: 200,
			UpdatedAtUnix:     200,
			OwnerNodeId:       "node-c",
			LogicalVersion:    1,
		},
	})
	if mergedOlder != 0 {
		t.Fatalf("expected older logical version to be ignored")
	}

	stillRemote := store.Get("payments", "p1")
	if len(stillRemote) != 1 || stillRemote[0].GetEndpoint() != "payments-remote:8080" {
		t.Fatalf("expected endpoint to remain payments-remote:8080")
	}
}

func TestServiceStoreListSince(t *testing.T) {
	store := NewServiceStore()
	store.Upsert(&apiv1.ServiceRecord{
		ServiceName:       "catalog",
		ServiceId:         "c1",
		Endpoint:          "catalog-1:8080",
		Version:           "v1",
		HealthStatus:      apiv1.HealthStatus_HEALTH_STATUS_SERVING,
		LastHeartbeatUnix: 10,
		UpdatedAtUnix:     10,
	})
	store.Upsert(&apiv1.ServiceRecord{
		ServiceName:       "catalog",
		ServiceId:         "c2",
		Endpoint:          "catalog-2:8080",
		Version:           "v1",
		HealthStatus:      apiv1.HealthStatus_HEALTH_STATUS_SERVING,
		LastHeartbeatUnix: 20,
		UpdatedAtUnix:     20,
	})

	recent := store.ListSince(15)
	if len(recent) != 1 {
		t.Fatalf("expected one recent record, got %d", len(recent))
	}
	if recent[0].GetServiceId() != "c2" {
		t.Fatalf("expected c2 to be returned by since filter")
	}
}

