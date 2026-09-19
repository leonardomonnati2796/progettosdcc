package storage

import (
	"testing"
	"time"

	apiv1 "github.com/leonardomonnati2796/distributed-service-registry/pkg/api"
)

func TestServiceStoreDeletionMarkerExpires(t *testing.T) {
	store := NewServiceStore()
	store.SetDeletionMarkerTTL(10 * time.Second)
	store.Upsert(&apiv1.ServiceMessage{
		ServiceName:  "catalog",
		Endpoint:     "catalog:8080",
		HealthStatus: apiv1.HealthStatus_HEALTH_STATUS_SERVING,
	})

	if !store.Remove("catalog", 100) {
		t.Fatal("expected service removal to create a deletion marker")
	}

	records := store.ListForSync()
	if len(records) != 1 || records[0].GetDeletionMarkerExpiresAtUnix() != 110 {
		t.Fatalf("unexpected deletion marker: %+v", records)
	}
	if len(store.List()) != 0 {
		t.Fatal("deletion marker must not appear in the service list")
	}
	if removed := store.PurgeExpiredDeletionMarkers(109); removed != 0 {
		t.Fatalf("expected marker to remain before expiry, removed %d", removed)
	}
	if removed := store.PurgeExpiredDeletionMarkers(110); removed != 1 {
		t.Fatalf("expected one expired marker, removed %d", removed)
	}
	if records := store.ListForSync(); len(records) != 0 {
		t.Fatalf("expected expired marker to be purged, found %d records", len(records))
	}
}

func TestServiceStoreDeletionMarkerWinsOverOlderRecord(t *testing.T) {
	store := NewServiceStore()
	store.SetDeletionMarkerTTL(time.Minute)
	store.Upsert(&apiv1.ServiceMessage{
		ServiceName:  "catalog",
		Endpoint:     "catalog:8080",
		HealthStatus: apiv1.HealthStatus_HEALTH_STATUS_SERVING,
		LamportClock: 4,
	})
	if !store.Remove("catalog", 100) {
		t.Fatal("expected service removal to create a deletion marker")
	}

	store.MergeRemote([]*apiv1.ServiceMessage{{
		ServiceName:  "catalog",
		Endpoint:     "old-catalog:8080",
		HealthStatus: apiv1.HealthStatus_HEALTH_STATUS_SERVING,
		LamportClock: 4,
	}})

	if records := store.List(); len(records) != 0 {
		t.Fatal("an older service record must not replace a deletion marker")
	}
}

func TestServiceStoreUsesLamportNodeIDAsTieBreaker(t *testing.T) {
	store := NewServiceStore()
	store.MergeRemote([]*apiv1.ServiceMessage{{
		ServiceName:   "catalog",
		Endpoint:      "catalog-a:8080",
		HealthStatus:  apiv1.HealthStatus_HEALTH_STATUS_SERVING,
		LamportClock:  7,
		LamportNodeId: "node-a",
	}})

	store.MergeRemote([]*apiv1.ServiceMessage{{
		ServiceName:   "catalog",
		Endpoint:      "catalog-z:8080",
		HealthStatus:  apiv1.HealthStatus_HEALTH_STATUS_SERVING,
		LamportClock:  7,
		LamportNodeId: "node-z",
	}})

	records := store.List()
	if len(records) != 1 || records[0].GetEndpoint() != "catalog-z:8080" {
		t.Fatalf("expected node-z record to win Lamport tie, got %+v", records)
	}
}
