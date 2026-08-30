package storage

import (
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	apiv1 "github.com/leonardomonnati2796/distributed-service-registry/pkg/api"
)

type PeerStore struct {
	mu       sync.RWMutex
	peers    map[string]*apiv1.NodeInfo
	onChange func()

	rngMu sync.Mutex
	rng   *rand.Rand
}

func NewPeerStore() *PeerStore {
	// Crea un nuovo peer store.
	return &PeerStore{
		peers: make(map[string]*apiv1.NodeInfo),
		rng:   rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (s *PeerStore) SetOnChange(onChange func()) {
	// Imposta on change.
	s.mu.Lock()
	s.onChange = onChange
	s.mu.Unlock()
}

func (s *PeerStore) ReplaceAll(peers []*apiv1.NodeInfo) {
	// Esegue la logica di replace all.
	replaced := make(map[string]*apiv1.NodeInfo, len(peers))
	for _, peer := range peers {
		if peer == nil {
			continue
		}
		nodeID := strings.TrimSpace(peer.GetNodeId())
		address := strings.TrimSpace(peer.GetGrpcAddress())
		if nodeID == "" || address == "" {
			continue
		}
		copyPeer := clonePeer(peer)
		if copyPeer.UpdatedAtUnix == 0 {
			copyPeer.UpdatedAtUnix = time.Now().Unix()
		}
		replaced[nodeID] = copyPeer
	}

	s.mu.Lock()
	s.peers = replaced
	s.mu.Unlock()
}

func (s *PeerStore) Upsert(peer *apiv1.NodeInfo) bool {
	// Esegue la logica di upsert.
	if peer == nil {
		return false
	}

	nodeID := strings.TrimSpace(peer.GetNodeId())
	address := strings.TrimSpace(peer.GetGrpcAddress())
	if nodeID == "" || address == "" {
		return false
	}

	incoming := &apiv1.NodeInfo{
		NodeId:        nodeID,
		GrpcAddress:   address,
		UpdatedAtUnix: peer.GetUpdatedAtUnix(),
	}
	if incoming.UpdatedAtUnix == 0 {
		incoming.UpdatedAtUnix = time.Now().Unix()
	}

	s.mu.Lock()

	current, exists := s.peers[nodeID]
	if !exists || shouldReplacePeer(current, incoming) {
		s.peers[nodeID] = incoming
		s.mu.Unlock()
		s.emitChange()
		return true
	}
	s.mu.Unlock()
	return false
}

func (s *PeerStore) UpsertSelf(nodeID, grpcAddress string, nowUnix int64) {
	// Esegue la logica di upsert self.
	if nowUnix == 0 {
		nowUnix = time.Now().Unix()
	}
	_ = s.Upsert(&apiv1.NodeInfo{
		NodeId:        strings.TrimSpace(nodeID),
		GrpcAddress:   strings.TrimSpace(grpcAddress),
		UpdatedAtUnix: nowUnix,
	})
}

func (s *PeerStore) MergeRemote(peers []*apiv1.NodeInfo) int {
	// Esegue la logica di merge remote.
	updated := 0
	for _, peer := range peers {
		if s.Upsert(peer) {
			updated++
		}
	}
	return updated
}

func (s *PeerStore) Remove(nodeID string) bool {
	// Rimuove esegue la logica della funzione..
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return false
	}

	s.mu.Lock()

	if _, exists := s.peers[nodeID]; !exists {
		s.mu.Unlock()
		return false
	}
	delete(s.peers, nodeID)
	s.mu.Unlock()
	s.emitChange()
	return true
}

func (s *PeerStore) List() []*apiv1.NodeInfo {
	// Elenca esegue la logica della funzione..
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*apiv1.NodeInfo, 0, len(s.peers))
	for _, peer := range s.peers {
		out = append(out, clonePeer(peer))
	}
	sortPeers(out)
	return out
}

func (s *PeerStore) ListSince(sinceUnix int64) []*apiv1.NodeInfo {
	// Elenca since.
	if sinceUnix <= 0 {
		return s.List()
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*apiv1.NodeInfo, 0, len(s.peers))
	for _, peer := range s.peers {
		if peer.GetUpdatedAtUnix() <= sinceUnix {
			continue
		}
		out = append(out, clonePeer(peer))
	}
	sortPeers(out)
	return out
}

func (s *PeerStore) RemoveStale(nowUnix int64, peerTimeoutSeconds int64, protectedIDs map[string]struct{}) int {
	// Rimuove stale.
	if peerTimeoutSeconds <= 0 {
		return 0
	}
	if nowUnix == 0 {
		nowUnix = time.Now().Unix()
	}

	s.mu.Lock()

	removed := 0
	for nodeID, peer := range s.peers {
		if _, protected := protectedIDs[nodeID]; protected {
			continue
		}
		if nowUnix-peer.GetUpdatedAtUnix() <= peerTimeoutSeconds {
			continue
		}
		delete(s.peers, nodeID)
		removed++
	}
	s.mu.Unlock()
	if removed > 0 {
		s.emitChange()
	}
	return removed
}

func (s *PeerStore) RandomPeers(max int, excludeIDs map[string]struct{}) []*apiv1.NodeInfo {
	// Esegue la logica di random peers.
	if max <= 0 {
		return nil
	}

	s.mu.RLock()
	candidates := make([]*apiv1.NodeInfo, 0, len(s.peers))
	for _, peer := range s.peers {
		if _, excluded := excludeIDs[peer.GetNodeId()]; excluded {
			continue
		}
		candidates = append(candidates, clonePeer(peer))
	}
	s.mu.RUnlock()

	if len(candidates) == 0 {
		return nil
	}

	s.rngMu.Lock()
	s.rng.Shuffle(len(candidates), func(i, j int) {
		candidates[i], candidates[j] = candidates[j], candidates[i]
	})
	s.rngMu.Unlock()

	if len(candidates) > max {
		candidates = candidates[:max]
	}
	sortPeers(candidates)
	return candidates
}

func shouldReplacePeer(current, incoming *apiv1.NodeInfo) bool {
	// Esegue la logica di should replace peer.
	if incoming.GetUpdatedAtUnix() != current.GetUpdatedAtUnix() {
		return incoming.GetUpdatedAtUnix() > current.GetUpdatedAtUnix()
	}
	if incoming.GetGrpcAddress() != current.GetGrpcAddress() {
		return incoming.GetGrpcAddress() > current.GetGrpcAddress()
	}
	return false
}

func clonePeer(peer *apiv1.NodeInfo) *apiv1.NodeInfo {
	// Esegue la logica di clone peer.
	if peer == nil {
		return nil
	}
	return &apiv1.NodeInfo{
		NodeId:        peer.GetNodeId(),
		GrpcAddress:   peer.GetGrpcAddress(),
		UpdatedAtUnix: peer.GetUpdatedAtUnix(),
	}
}

func sortPeers(peers []*apiv1.NodeInfo) {
	// Esegue la logica di sort peers.
	sort.Slice(peers, func(i, j int) bool {
		if peers[i].GetNodeId() == peers[j].GetNodeId() {
			return peers[i].GetGrpcAddress() < peers[j].GetGrpcAddress()
		}
		return peers[i].GetNodeId() < peers[j].GetNodeId()
	})
}

func (s *PeerStore) emitChange() {
	// Esegue la logica di emit change.
	s.mu.RLock()
	onChange := s.onChange
	s.mu.RUnlock()
	if onChange != nil {
		onChange()
	}
}
