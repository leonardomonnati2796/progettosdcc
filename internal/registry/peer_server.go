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

type RegistryPeerServer struct {
	apiv1.UnimplementedRegistryPeerServer

	store            *storage.ServiceStore
	peerStore        *storage.PeerStore
	nodeID           string
	advertiseAddress string
	now              func() time.Time
}

func NewRegistryPeerServer(store *storage.ServiceStore, peerStore *storage.PeerStore, nodeID string, advertiseAddress string) *RegistryPeerServer {
	if peerStore == nil {
		peerStore = storage.NewPeerStore()
	}
	return &RegistryPeerServer{
		store:            store,
		peerStore:        peerStore,
		nodeID:           strings.TrimSpace(nodeID),
		advertiseAddress: strings.TrimSpace(advertiseAddress),
		now:              time.Now,
	}
}

func (s *RegistryPeerServer) JoinCluster(_ context.Context, req *apiv1.JoinClusterRequest) (*apiv1.JoinClusterResponse, error) {
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

	response := &apiv1.JoinClusterResponse{
		Records: s.store.ListForSync(),
		Peers:   s.peerStore.List(),
	}
	return response, nil
}

func (s *RegistryPeerServer) GossipSync(_ context.Context, req *apiv1.GossipSyncRequest) (*apiv1.GossipSyncResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	nowUnix := s.now().Unix()
	s.peerStore.UpsertSelf(s.nodeID, s.advertiseAddress, nowUnix)
	s.peerStore.MergeRemote(req.GetPeers())
	s.store.MergeRemote(req.GetRecords())

	return &apiv1.GossipSyncResponse{
		Accepted:       true,
		ReceivedAtUnix: nowUnix,
	}, nil
}

func (s *RegistryPeerServer) LeaveCluster(_ context.Context, req *apiv1.JoinClusterRequest) (*apiv1.GossipSyncResponse, error) {
	if req == nil || req.GetNode() == nil {
		return nil, status.Error(codes.InvalidArgument, "node is required")
	}

	nodeID := strings.TrimSpace(req.GetNode().GetNodeId())
	if nodeID == "" {
		return nil, status.Error(codes.InvalidArgument, "node.node_id is required")
	}

	removed := s.peerStore.Remove(nodeID)
	return &apiv1.GossipSyncResponse{
		Accepted:       removed,
		ReceivedAtUnix: s.now().Unix(),
	}, nil
}

func (s *RegistryPeerServer) PullState(_ context.Context, req *apiv1.PullStateRequest) (*apiv1.PullStateResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	sinceUnix := req.GetSinceUnix()
	nowUnix := s.now().Unix()
	s.peerStore.UpsertSelf(s.nodeID, s.advertiseAddress, nowUnix)

	return &apiv1.PullStateResponse{
		Records: s.store.ListSince(sinceUnix),
		Peers:   s.peerStore.ListSince(sinceUnix),
	}, nil
}

