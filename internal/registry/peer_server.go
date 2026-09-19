package registry

import (
	"context"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/leonardomonnati2796/distributed-service-registry/internal/storage"
	apiv1 "github.com/leonardomonnati2796/distributed-service-registry/pkg/api"
)

type RegPeerServer struct {
	apiv1.UnimplementedRegPeerServer

	store            *storage.ServiceStore
	peerStore        *storage.PeerStore
	nodeID           string
	advertiseAddress string
	now              func() time.Time
}

func NewRegPeerServer(store *storage.ServiceStore, peerStore *storage.PeerStore, nodeID string, advertiseAddress string) *RegPeerServer {
	// Crea un nuovo registry peer server.
	if peerStore == nil {
		peerStore = storage.NewPeerStore()
	}
	return &RegPeerServer{
		store:            store,
		peerStore:        peerStore,
		nodeID:           strings.TrimSpace(nodeID),
		advertiseAddress: strings.TrimSpace(advertiseAddress),
		now:              time.Now,
	}
}

func (s *RegPeerServer) JoinNode(_ context.Context, req *apiv1.JoinNodeRequest) (*apiv1.JoinNodeResponse, error) {
	// Unisce il nodo al cluster.
	if req == nil || req.GetNode() == nil {
		return nil, status.Error(codes.InvalidArgument, "node is required")
	}
	nowUnix := s.now().Unix()

	peer := req.GetNode()
	if peer.GetUpdatedAtUnix() == 0 {
		peer = &apiv1.NodeInfo{
			NodeId:        peer.GetNodeId(),
			GrpcAddress:   peer.GetGrpcAddress(),
			UpdatedAtUnix: nowUnix,
		}
	}
	_ = s.peerStore.Upsert(peer)
	s.peerStore.UpsertSelf(s.nodeID, s.advertiseAddress, nowUnix)

	response := &apiv1.JoinNodeResponse{
		Records: s.store.ListForSync(),
		Peers:   s.peerStore.List(),
	}
	return response, nil
}

func (s *RegPeerServer) GossipUpd(_ context.Context, req *apiv1.GossipUpdRequest) (*apiv1.GossipUpdResponse, error) {
	// Gestisce la propagazione gossip tra i nodi.
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	nowUnix := s.now().Unix()
	s.peerStore.UpsertSelf(s.nodeID, s.advertiseAddress, nowUnix)
	s.peerStore.MergeRemote(req.GetPeers())
	s.store.MergeRemote(req.GetRecords())

	return &apiv1.GossipUpdResponse{
		Accepted:       true,
		ReceivedAtUnix: nowUnix,
	}, nil
}

func (s *RegPeerServer) LeaveCluster(_ context.Context, req *apiv1.JoinNodeRequest) (*apiv1.GossipUpdResponse, error) {
	// Esegue la logica di leave cluster.
	if req == nil || req.GetNode() == nil {
		return nil, status.Error(codes.InvalidArgument, "node is required")
	}

	nodeID := strings.TrimSpace(req.GetNode().GetNodeId())
	if nodeID == "" {
		return nil, status.Error(codes.InvalidArgument, "node.node_id is required")
	}

	removed := s.peerStore.Remove(nodeID)
	return &apiv1.GossipUpdResponse{
		Accepted:       removed,
		ReceivedAtUnix: s.now().Unix(),
	}, nil
}

func (s *RegPeerServer) AntyEntropyPull(_ context.Context, req *apiv1.AntyEntropyPullRequest) (*apiv1.AntyEntropyPullResponse, error) {
	// Esegue la logica di pull state.
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	sinceUnix := req.GetSinceUnix()
	nowUnix := s.now().Unix()
	s.peerStore.UpsertSelf(s.nodeID, s.advertiseAddress, nowUnix)

	return &apiv1.AntyEntropyPullResponse{
		Records: s.store.ListSince(sinceUnix),
		Peers:   s.peerStore.ListSince(sinceUnix),
	}, nil
}
