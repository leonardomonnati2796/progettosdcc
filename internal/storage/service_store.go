package storage

import (
	"sort"
	"strings"
	"sync"
	"time"

	apiv1 "github.com/leonardomonnati2796/distributed-service-registry/pkg/api"
)

type ServiceStore struct {
	mu                       sync.RWMutex
	records                  map[string]*apiv1.ServiceMessage
	requestResults           map[string]*apiv1.ServiceDeregResponse
	onChange                 func()
	deletionMarkerTTLSeconds int64
}

const (
	deletionMarkerEndpoint = ""
)

func NewServiceStore() *ServiceStore {
	// Crea un nuovo service store.
	return &ServiceStore{
		records:                  make(map[string]*apiv1.ServiceMessage),
		requestResults:           make(map[string]*apiv1.ServiceDeregResponse),
		deletionMarkerTTLSeconds: 180,
	}
}

func (s *ServiceStore) SetDeletionMarkerTTL(ttl time.Duration) {
	seconds := int64(ttl / time.Second)
	if seconds <= 0 {
		seconds = 1
	}
	s.mu.Lock()
	s.deletionMarkerTTLSeconds = seconds
	s.mu.Unlock()
}

func (s *ServiceStore) SetOnChange(onChange func()) {
	// Imposta on change.
	s.mu.Lock()
	s.onChange = onChange
	s.mu.Unlock()
}

func (s *ServiceStore) RecordDeregisterResult(requestID string, response *apiv1.ServiceDeregResponse) {
	// Esegue la logica di record deregister result.
	if requestID == "" || response == nil {
		return
	}

	s.mu.Lock()
	if s.requestResults == nil {
		s.requestResults = make(map[string]*apiv1.ServiceDeregResponse)
	}
	s.requestResults[requestID] = &apiv1.ServiceDeregResponse{
		Accepted: response.GetAccepted(),
		Message:  response.GetMessage(),
	}
	s.mu.Unlock()
}

func (s *ServiceStore) GetDeregisterResult(requestID string) (*apiv1.ServiceDeregResponse, bool) {
	// Recupera deregister result.
	if requestID == "" {
		return nil, false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.requestResults == nil {
		return nil, false
	}
	resp, ok := s.requestResults[requestID]
	if !ok || resp == nil {
		return nil, false
	}
	return &apiv1.ServiceDeregResponse{Accepted: resp.GetAccepted(), Message: resp.GetMessage()}, true
}

func (s *ServiceStore) ReplaceAll(records []*apiv1.ServiceMessage) {
	replaced := make(map[string]*apiv1.ServiceMessage, len(records))
	for _, record := range records {
		normalized, ok := normalizeRecord(record)
		if !ok {
			continue
		}
		if normalized.LamportClock == 0 {
			normalized.LamportClock = 1
		}
		replaced[recordKey(normalized.GetServiceName())] = normalized
	}

	s.mu.Lock()
	s.records = replaced
	s.mu.Unlock()
}

func (s *ServiceStore) Upsert(record *apiv1.ServiceMessage) *apiv1.ServiceMessage {
	normalized, ok := normalizeRecord(record)
	if !ok {
		return nil
	}

	key := recordKey(normalized.GetServiceName())

	s.mu.Lock()

	existing, exists := s.records[key]
	if !exists {
		if normalized.LamportClock == 0 {
			normalized.LamportClock = 1
		}
		s.records[key] = normalized
		out := cloneRecord(normalized)
		s.mu.Unlock()
		s.emitChange()
		return out
	}

	if normalized.LamportClock <= existing.LamportClock {
		normalized.LamportClock = existing.LamportClock + 1
	}
	s.records[key] = normalized
	out := cloneRecord(normalized)
	s.mu.Unlock()
	s.emitChange()
	return out
}

func (s *ServiceStore) Remove(serviceName string, nowUnix int64) bool {
	// Rimuove esegue la logica della funzione..
	key := recordKey(serviceName)
	if nowUnix == 0 {
		nowUnix = 1
	}

	s.mu.Lock()

	existing, exists := s.records[key]
	if !exists {
		s.mu.Unlock()
		return false
	}
	if isDeletionMarker(existing) {
		s.mu.Unlock()
		return false
	}
	deletionMarker := cloneRecord(existing)
	deletionMarker.Endpoint = deletionMarkerEndpoint
	deletionMarker.HealthStatus = apiv1.HealthStatus_HEALTH_STATUS_NOT_SERVING
	deletionMarker.LamportClock++
	deletionMarker.DeletionMarkerExpiresAtUnix = nowUnix + s.deletionMarkerTTLSeconds
	s.records[key] = deletionMarker
	s.mu.Unlock()
	s.emitChange()
	return true
}

func (s *ServiceStore) Get(serviceName string) []*apiv1.ServiceMessage {
	// Recupera esegue la logica della funzione..
	normalizedName := strings.TrimSpace(serviceName)
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, exists := s.records[recordKey(normalizedName)]
	if !exists || isDeletionMarker(record) {
		return nil
	}
	return []*apiv1.ServiceMessage{cloneRecord(record)}
}

func (s *ServiceStore) GetForSync(serviceName string) *apiv1.ServiceMessage {
	key := recordKey(serviceName)
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneRecord(s.records[key])
}

func (s *ServiceStore) List() []*apiv1.ServiceMessage {
	// Elenca esegue la logica della funzione..
	all := s.ListForSync()
	out := make([]*apiv1.ServiceMessage, 0, len(all))
	for _, record := range all {
		if isDeletionMarker(record) {
			continue
		}
		out = append(out, record)
	}
	return out
}

func (s *ServiceStore) ListForSync() []*apiv1.ServiceMessage {
	// Elenca for sync.
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*apiv1.ServiceMessage, 0, len(s.records))
	for _, record := range s.records {
		out = append(out, cloneRecord(record))
	}
	sortRecords(out)
	return out
}

func (s *ServiceStore) ListSince(sinceUnix int64) []*apiv1.ServiceMessage {
	// Elenca since.
	return s.List()
}

func (s *ServiceStore) PurgeExpiredDeletionMarkers(nowUnix int64) int {
	if nowUnix == 0 {
		nowUnix = time.Now().Unix()
	}

	s.mu.Lock()
	removed := 0
	for key, record := range s.records {
		if !isDeletionMarker(record) || record.GetDeletionMarkerExpiresAtUnix() <= 0 || record.GetDeletionMarkerExpiresAtUnix() > nowUnix {
			continue
		}
		delete(s.records, key)
		removed++
	}
	s.mu.Unlock()
	if removed > 0 {
		s.emitChange()
	}
	return removed
}

func (s *ServiceStore) MergeRemote(records []*apiv1.ServiceMessage) int {
	// Esegue la logica di merge remote.
	if len(records) == 0 {
		return 0
	}

	s.mu.Lock()

	updated := 0
	for _, remote := range records {
		if remote == nil {
			continue
		}

		incoming, ok := normalizeRecord(remote)
		if !ok {
			continue
		}

		if incoming.LamportClock == 0 {
			incoming.LamportClock = 1
		}

		key := recordKey(incoming.GetServiceName())
		current, exists := s.records[key]
		if !exists {
			s.records[key] = incoming
			updated++
			continue
		}

		if shouldReplaceRecord(current, incoming) {
			s.records[key] = incoming
			updated++
		}
	}

	s.mu.Unlock()
	if updated > 0 {
		s.emitChange()
	}
	return updated
}

func (s *ServiceStore) emitChange() {
	// Esegue la logica di emit change.
	s.mu.RLock()
	onChange := s.onChange
	s.mu.RUnlock()
	if onChange != nil {
		onChange()
	}
}

func recordKey(serviceName string) string {
	return strings.TrimSpace(serviceName)
}

func normalizeRecord(record *apiv1.ServiceMessage) (*apiv1.ServiceMessage, bool) {
	if record == nil {
		return nil, false
	}

	normalized := cloneRecord(record)
	normalized.ServiceName = strings.TrimSpace(normalized.GetServiceName())
	if normalized.ServiceName == "" {
		return nil, false
	}
	normalized.Endpoint = strings.TrimSpace(normalized.GetEndpoint())
	return normalized, true
}

func cloneRecord(record *apiv1.ServiceMessage) *apiv1.ServiceMessage {
	// Esegue la logica di clone record.
	if record == nil {
		return nil
	}
	return &apiv1.ServiceMessage{
		ServiceName:                 record.GetServiceName(),
		Endpoint:                    record.GetEndpoint(),
		HealthStatus:                record.GetHealthStatus(),
		LamportClock:                record.GetLamportClock(),
		LamportNodeId:               record.GetLamportNodeId(),
		DeletionMarkerExpiresAtUnix: record.GetDeletionMarkerExpiresAtUnix(),
	}
}

func sortRecords(records []*apiv1.ServiceMessage) {
	// Esegue la logica di sort records.
	sort.Slice(records, func(i, j int) bool {
		left := records[i]
		right := records[j]
		if left.GetServiceName() == right.GetServiceName() {
			return false
		}
		return left.GetServiceName() < right.GetServiceName()
	})
}

func shouldReplaceRecord(local, incoming *apiv1.ServiceMessage) bool {
	// Esegue la logica di should replace record.
	if incoming.GetLamportClock() != local.GetLamportClock() {
		return incoming.GetLamportClock() > local.GetLamportClock()
	}
	if incoming.GetLamportNodeId() != local.GetLamportNodeId() {
		return incoming.GetLamportNodeId() > local.GetLamportNodeId()
	}
	if incoming.GetHealthStatus() != local.GetHealthStatus() {
		return incoming.GetHealthStatus() > local.GetHealthStatus()
	}
	if incoming.GetEndpoint() != local.GetEndpoint() {
		return incoming.GetEndpoint() > local.GetEndpoint()
	}
	return false
}

func isDeletionMarker(record *apiv1.ServiceMessage) bool {
	// Verifica la condizione richiesta.
	if record == nil {
		return false
	}
	return record.GetHealthStatus() == apiv1.HealthStatus_HEALTH_STATUS_NOT_SERVING &&
		record.GetEndpoint() == deletionMarkerEndpoint &&
		record.GetEndpoint() == ""
}
