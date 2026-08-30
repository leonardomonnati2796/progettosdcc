package storage

import (
	"sort"
	"strings"
	"sync"

	apiv1 "github.com/leonardomonnati2796/distributed-service-registry/pkg/api"
)

type ServiceStore struct {
	mu             sync.RWMutex
	records        map[string]*apiv1.ServiceRecord
	requestResults map[string]*apiv1.DeregisterServiceResponse
	onChange       func()
}

const (
	tombstoneEndpoint = ""
	tombstoneVersion  = ""
)

func NewServiceStore() *ServiceStore {
	// Crea un nuovo service store.
	return &ServiceStore{
		records:        make(map[string]*apiv1.ServiceRecord),
		requestResults: make(map[string]*apiv1.DeregisterServiceResponse),
	}
}

func (s *ServiceStore) SetOnChange(onChange func()) {
	// Imposta on change.
	s.mu.Lock()
	s.onChange = onChange
	s.mu.Unlock()
}

func (s *ServiceStore) RecordDeregisterResult(requestID string, response *apiv1.DeregisterServiceResponse) {
	// Esegue la logica di record deregister result.
	if requestID == "" || response == nil {
		return
	}

	s.mu.Lock()
	if s.requestResults == nil {
		s.requestResults = make(map[string]*apiv1.DeregisterServiceResponse)
	}
	s.requestResults[requestID] = &apiv1.DeregisterServiceResponse{
		Accepted: response.GetAccepted(),
		Message:  response.GetMessage(),
	}
	s.mu.Unlock()
}

func (s *ServiceStore) GetDeregisterResult(requestID string) (*apiv1.DeregisterServiceResponse, bool) {
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
	return &apiv1.DeregisterServiceResponse{Accepted: resp.GetAccepted(), Message: resp.GetMessage()}, true
}

func (s *ServiceStore) ReplaceAll(records []*apiv1.ServiceRecord) {
	// Esegue la logica di replace all.
	replaced := make(map[string]*apiv1.ServiceRecord, len(records))
	for _, record := range records {
		if record == nil {
			continue
		}
		serviceName := strings.TrimSpace(record.GetServiceName())
		serviceID := strings.TrimSpace(record.GetServiceId())
		if serviceName == "" || serviceID == "" {
			continue
		}
		copyRecord := cloneRecord(record)
		if copyRecord.LogicalVersion == 0 {
			copyRecord.LogicalVersion = 1
		}
		replaced[recordKey(serviceName, serviceID)] = copyRecord
	}

	s.mu.Lock()
	s.records = replaced
	s.mu.Unlock()
}

func (s *ServiceStore) Upsert(record *apiv1.ServiceRecord) *apiv1.ServiceRecord {
	// Esegue la logica di upsert.
	if record == nil {
		return nil
	}

	key := recordKey(record.GetServiceName(), record.GetServiceId())
	copyRecord := cloneRecord(record)

	s.mu.Lock()

	existing, exists := s.records[key]
	if !exists {
		if copyRecord.LogicalVersion == 0 {
			copyRecord.LogicalVersion = 1
		}
		s.records[key] = copyRecord
		out := cloneRecord(copyRecord)
		s.mu.Unlock()
		s.emitChange()
		return out
	}

	if copyRecord.LogicalVersion <= existing.LogicalVersion {
		copyRecord.LogicalVersion = existing.LogicalVersion + 1
	}
	s.records[key] = copyRecord
	out := cloneRecord(copyRecord)
	s.mu.Unlock()
	s.emitChange()
	return out
}

func (s *ServiceStore) Remove(serviceName, serviceID string, nowUnix int64) bool {
	// Rimuove esegue la logica della funzione..
	key := recordKey(serviceName, serviceID)
	if nowUnix == 0 {
		nowUnix = 1
	}

	s.mu.Lock()

	existing, exists := s.records[key]
	if !exists {
		s.mu.Unlock()
		return false
	}
	if isTombstone(existing) {
		s.mu.Unlock()
		return false
	}
	tombstone := cloneRecord(existing)
	tombstone.Endpoint = tombstoneEndpoint
	tombstone.Version = tombstoneVersion
	tombstone.HealthStatus = apiv1.HealthStatus_HEALTH_STATUS_NOT_SERVING
	tombstone.LogicalVersion++
	s.records[key] = tombstone
	s.mu.Unlock()
	s.emitChange()
	return true
}

func (s *ServiceStore) UpdateHeartbeat(serviceName, serviceID string, status apiv1.HealthStatus, heartbeatUnix int64) (*apiv1.ServiceRecord, bool) {
	// Aggiorna heartbeat.
	key := recordKey(serviceName, serviceID)

	s.mu.Lock()

	existing, exists := s.records[key]
	if !exists || isTombstone(existing) {
		s.mu.Unlock()
		return nil, false
	}

	existing.HealthStatus = status
	existing.LastHeartbeatUnix = heartbeatUnix
	existing.LogicalVersion++

	out := cloneRecord(existing)
	s.mu.Unlock()
	s.emitChange()
	return out, true
}

func (s *ServiceStore) Get(serviceName, serviceID string) []*apiv1.ServiceRecord {
	// Recupera esegue la logica della funzione..
	normalizedName := strings.TrimSpace(serviceName)
	normalizedID := strings.TrimSpace(serviceID)

	s.mu.RLock()
	defer s.mu.RUnlock()

	if normalizedID != "" {
		record, exists := s.records[recordKey(normalizedName, normalizedID)]
		if !exists || isTombstone(record) {
			return nil
		}
		return []*apiv1.ServiceRecord{cloneRecord(record)}
	}

	matches := make([]*apiv1.ServiceRecord, 0)
	for _, record := range s.records {
		if record.GetServiceName() == normalizedName && !isTombstone(record) {
			matches = append(matches, cloneRecord(record))
		}
	}
	sortRecords(matches)
	return matches
}

func (s *ServiceStore) List() []*apiv1.ServiceRecord {
	// Elenca esegue la logica della funzione..
	all := s.ListForSync()
	out := make([]*apiv1.ServiceRecord, 0, len(all))
	for _, record := range all {
		if isTombstone(record) {
			continue
		}
		out = append(out, record)
	}
	return out
}

func (s *ServiceStore) ListForSync() []*apiv1.ServiceRecord {
	// Elenca for sync.
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*apiv1.ServiceRecord, 0, len(s.records))
	for _, record := range s.records {
		out = append(out, cloneRecord(record))
	}
	sortRecords(out)
	return out
}

func (s *ServiceStore) ListSince(sinceUnix int64) []*apiv1.ServiceRecord {
	// Elenca since.
	return s.List()
}

func (s *ServiceStore) MergeRemote(records []*apiv1.ServiceRecord) int {
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

		serviceName := strings.TrimSpace(remote.GetServiceName())
		serviceID := strings.TrimSpace(remote.GetServiceId())
		if serviceName == "" || serviceID == "" {
			continue
		}

		incoming := cloneRecord(remote)
		incoming.ServiceName = serviceName
		incoming.ServiceId = serviceID
		incoming.Endpoint = strings.TrimSpace(incoming.GetEndpoint())
		incoming.Version = strings.TrimSpace(incoming.GetVersion())
		incoming.OwnerNodeId = strings.TrimSpace(incoming.GetOwnerNodeId())
		if incoming.LogicalVersion == 0 {
			incoming.LogicalVersion = 1
		}

		key := recordKey(serviceName, serviceID)
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

func (s *ServiceStore) MarkStale(nowUnix int64, heartbeatTTLSeconds int64) int {
	// Esegue la logica di mark stale.
	if heartbeatTTLSeconds <= 0 {
		return 0
	}

	s.mu.Lock()

	updated := 0
	for _, record := range s.records {
		if isTombstone(record) {
			continue
		}
		if nowUnix-record.GetLastHeartbeatUnix() <= heartbeatTTLSeconds {
			continue
		}
		if record.GetHealthStatus() == apiv1.HealthStatus_HEALTH_STATUS_NOT_SERVING {
			continue
		}
		record.HealthStatus = apiv1.HealthStatus_HEALTH_STATUS_NOT_SERVING
		record.LogicalVersion++
		updated++
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

func recordKey(serviceName, serviceID string) string {
	// Esegue la logica di record key.
	return strings.TrimSpace(serviceName) + "|" + strings.TrimSpace(serviceID)
}

func cloneRecord(record *apiv1.ServiceRecord) *apiv1.ServiceRecord {
	// Esegue la logica di clone record.
	if record == nil {
		return nil
	}
	return &apiv1.ServiceRecord{
		ServiceName:       record.GetServiceName(),
		ServiceId:         record.GetServiceId(),
		Endpoint:          record.GetEndpoint(),
		Version:           record.GetVersion(),
		HealthStatus:      record.GetHealthStatus(),
		LastHeartbeatUnix: record.GetLastHeartbeatUnix(),
		OwnerNodeId:       record.GetOwnerNodeId(),
		LogicalVersion:    record.GetLogicalVersion(),
	}
}

func sortRecords(records []*apiv1.ServiceRecord) {
	// Esegue la logica di sort records.
	sort.Slice(records, func(i, j int) bool {
		left := records[i]
		right := records[j]
		if left.GetServiceName() == right.GetServiceName() {
			return left.GetServiceId() < right.GetServiceId()
		}
		return left.GetServiceName() < right.GetServiceName()
	})
}

func shouldReplaceRecord(local, incoming *apiv1.ServiceRecord) bool {
	// Esegue la logica di should replace record.
	if incoming.GetLogicalVersion() != local.GetLogicalVersion() {
		return incoming.GetLogicalVersion() > local.GetLogicalVersion()
	}
	if incoming.GetLastHeartbeatUnix() != local.GetLastHeartbeatUnix() {
		return incoming.GetLastHeartbeatUnix() > local.GetLastHeartbeatUnix()
	}
	if incoming.GetHealthStatus() != local.GetHealthStatus() {
		return incoming.GetHealthStatus() > local.GetHealthStatus()
	}
	if incoming.GetOwnerNodeId() != local.GetOwnerNodeId() {
		return incoming.GetOwnerNodeId() > local.GetOwnerNodeId()
	}
	if incoming.GetEndpoint() != local.GetEndpoint() {
		return incoming.GetEndpoint() > local.GetEndpoint()
	}
	if incoming.GetVersion() != local.GetVersion() {
		return incoming.GetVersion() > local.GetVersion()
	}
	return false
}

func isTombstone(record *apiv1.ServiceRecord) bool {
	// Verifica la condizione richiesta.
	if record == nil {
		return false
	}
	return record.GetHealthStatus() == apiv1.HealthStatus_HEALTH_STATUS_NOT_SERVING &&
		record.GetEndpoint() == tombstoneEndpoint &&
		record.GetVersion() == tombstoneVersion
}
