package gossip

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/leonardomonnati2796/distributed-service-registry/internal/config"
	"github.com/leonardomonnati2796/distributed-service-registry/internal/registry"
	"github.com/leonardomonnati2796/distributed-service-registry/internal/storage"
	apiv1 "github.com/leonardomonnati2796/distributed-service-registry/pkg/api"
)

type Runtime struct {
	nodeID           string
	advertiseAddress string
	seedPeers        []string

	gossipInterval    time.Duration
	startDelay        time.Duration
	gossipGatePath    string
	reconcileInterval time.Duration
	peerTimeout       time.Duration
	maxGossipFanout   int
	dialTimeout       time.Duration

	serviceStore *storage.ServiceStore
	peerStore    *storage.PeerStore

	lastReconcileUnix atomic.Int64

	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

func NewRuntime(cfg *config.RegistryConfig, serviceStore *storage.ServiceStore, peerStore *storage.PeerStore) *Runtime {
	// Crea un nuovo runtime.
	if peerStore == nil {
		peerStore = storage.NewPeerStore()
	}

	gossipInterval := time.Duration(cfg.Cluster.GossipIntervalSeconds) * time.Second
	if gossipInterval <= 0 {
		gossipInterval = 3 * time.Second
	}

	startDelay := time.Duration(cfg.Cluster.GossipStartDelaySeconds) * time.Second
	if startDelay < 0 {
		startDelay = 0
	}
	gossipGatePath := strings.TrimSpace(cfg.Cluster.GossipGatePath)

	reconcileInterval := time.Duration(cfg.Cluster.ReconcileIntervalSeconds) * time.Second
	if reconcileInterval <= 0 {
		reconcileInterval = 15 * time.Second
	}

	peerTimeout := time.Duration(cfg.Cluster.PeerTimeoutSeconds) * time.Second
	if peerTimeout <= 0 {
		peerTimeout = 10 * time.Second
	}

	maxFanout := cfg.Cluster.MaxGossipFanout
	if maxFanout <= 0 {
		maxFanout = 2
	}

	return &Runtime{
		nodeID:            strings.TrimSpace(cfg.Node.ID),
		advertiseAddress:  strings.TrimSpace(cfg.Node.AdvertiseAddress),
		seedPeers:         append([]string(nil), cfg.Cluster.SeedPeers...),
		gossipInterval:    gossipInterval,
		startDelay:        startDelay,
		gossipGatePath:    gossipGatePath,
		reconcileInterval: reconcileInterval,
		peerTimeout:       peerTimeout,
		maxGossipFanout:   maxFanout,
		dialTimeout:       2 * time.Second,
		serviceStore:      serviceStore,
		peerStore:         peerStore,
		stopCh:            make(chan struct{}),
	}
}

func (r *Runtime) Start() {
	// Avvia l'esecuzione del componente.
	nowUnix := time.Now().Unix()
	r.peerStore.UpsertSelf(r.nodeID, r.advertiseAddress, nowUnix)
	r.lastReconcileUnix.Store(nowUnix)

	r.wg.Add(4)
	go r.bootstrapLoop()
	go r.gossipLoop()
	go r.reconcileLoop()
	go r.peerSweepLoop()
}

func (r *Runtime) Stop() {
	// Arresta l'esecuzione corrente.
	r.stopOnce.Do(func() {
		close(r.stopCh)
	})
	r.wg.Wait()
}

func (r *Runtime) bootstrapLoop() {
	// Esegue il bootstrap iniziale del sistema.
	defer r.wg.Done()

	if !r.waitForGossipGate() {
		return
	}
	if !r.waitForStartDelay() {
		return
	}

	r.bootstrapFromSeeds()
	ticker := time.NewTicker(r.reconcileInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.stopCh:
			return
		case <-ticker.C:
			r.bootstrapFromSeeds()
		}
	}
}

func (r *Runtime) gossipLoop() {
	// Gestisce la propagazione gossip tra i nodi.
	defer r.wg.Done()

	if !r.waitForGossipGate() {
		return
	}
	if !r.waitForStartDelay() {
		return
	}

	ticker := time.NewTicker(r.gossipInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.stopCh:
			return
		case <-ticker.C:
			r.runGossipRound()
		}
	}
}

func (r *Runtime) reconcileLoop() {
	// Riconcilia lo stato tra i nodi.
	defer r.wg.Done()

	if !r.waitForGossipGate() {
		return
	}
	if !r.waitForStartDelay() {
		return
	}

	ticker := time.NewTicker(r.reconcileInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.stopCh:
			return
		case <-ticker.C:
			r.runReconcileRound()
		}
	}
}

func (r *Runtime) peerSweepLoop() {
	// Esegue la logica di peer sweep loop.
	defer r.wg.Done()

	if !r.waitForGossipGate() {
		return
	}
	if !r.waitForStartDelay() {
		return
	}

	interval := r.peerTimeout / 2
	if interval < time.Second {
		interval = time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-r.stopCh:
			return
		case <-ticker.C:
			r.peerStore.UpsertSelf(r.nodeID, r.advertiseAddress, time.Now().Unix())
			r.peerStore.RemoveStale(time.Now().Unix(), int64(r.peerTimeout.Seconds()), map[string]struct{}{
				r.nodeID: {},
			})
		}
	}
}

func (r *Runtime) bootstrapFromSeeds() {
	// Esegue il bootstrap iniziale del sistema.
	for _, seedAddress := range r.seedPeers {
		address := strings.TrimSpace(seedAddress)
		if address == "" || address == r.advertiseAddress {
			continue
		}
		if err := r.joinPeer(address); err != nil {
			continue
		}
	}
}

func (r *Runtime) joinPeer(address string) error {
	// Unisce il nodo al cluster.
	nowUnix := time.Now().Unix()
	request := &apiv1.JoinClusterRequest{
		Node: &apiv1.NodeInfo{
			NodeId:        r.nodeID,
			GrpcAddress:   r.advertiseAddress,
			UpdatedAtUnix: nowUnix,
		},
	}

	return r.withPeerClient(address, func(ctx context.Context, client apiv1.RegistryPeerClient) error {
		response, err := client.JoinCluster(ctx, request)
		if err != nil {
			return err
		}
		r.peerStore.MergeRemote(response.GetPeers())
		r.serviceStore.MergeRemote(response.GetRecords())
		return nil
	})
}

func (r *Runtime) waitForGossipGate() bool {
	// Attende il completamento della condizione richiesta.
	if r.gossipGatePath == "" {
		return true
	}

	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		if _, err := os.Stat(r.gossipGatePath); err == nil {
			return true
		} else if !os.IsNotExist(err) {
			log.Printf("gossip gate check failed for %s: %v", r.gossipGatePath, err)
		}

		select {
		case <-r.stopCh:
			return false
		case <-ticker.C:
		}
	}
}

func (r *Runtime) waitForStartDelay() bool {
	// Attende il completamento della condizione richiesta.
	if r.startDelay <= 0 {
		return true
	}

	timer := time.NewTimer(r.startDelay)
	defer timer.Stop()

	select {
	case <-timer.C:
		return true
	case <-r.stopCh:
		return false
	}
}

func (r *Runtime) runGossipRound() {
	// Esegue il comando richiesto.
	r.peerStore.UpsertSelf(r.nodeID, r.advertiseAddress, time.Now().Unix())
	targets := r.peerStore.RandomPeers(r.maxGossipFanout, map[string]struct{}{r.nodeID: {}})
	if len(targets) == 0 {
		r.bootstrapFromSeeds()
		return
	}

	records := r.serviceStore.ListForSync()
	peers := r.peerStore.List()
	nowUnix := time.Now().Unix()

	request := &apiv1.GossipSyncRequest{
		SourceNodeId: r.nodeID,
		Records:      records,
		Peers:        peers,
		SentAtUnix:   nowUnix,
	}

	for _, target := range targets {
		if target.GetGrpcAddress() == "" {
			continue
		}
		_ = r.sendGossip(target.GetGrpcAddress(), request)
	}
}

func (r *Runtime) runReconcileRound() {
	// Esegue il comando richiesto.
	r.peerStore.UpsertSelf(r.nodeID, r.advertiseAddress, time.Now().Unix())
	targets := r.peerStore.RandomPeers(1, map[string]struct{}{r.nodeID: {}})
	if len(targets) == 0 {
		r.bootstrapFromSeeds()
		return
	}

	sinceUnix := r.lastReconcileUnix.Load()
	for _, target := range targets {
		if target.GetGrpcAddress() == "" {
			continue
		}
		if err := r.pullState(target.GetGrpcAddress(), sinceUnix); err != nil {
			continue
		}
		r.lastReconcileUnix.Store(time.Now().Unix())
		return
	}
}

func (r *Runtime) GracefulLeave() {
	// Esegue la logica di graceful leave.
	request := &apiv1.JoinClusterRequest{
		Node: &apiv1.NodeInfo{
			NodeId:        r.nodeID,
			GrpcAddress:   r.advertiseAddress,
			UpdatedAtUnix: time.Now().Unix(),
		},
	}

	for _, address := range r.leaveTargets() {
		if err := r.leaveCluster(address, request); err != nil {
			log.Printf("graceful leave failed for %s: %v", address, err)
		}
	}
	_ = r.peerStore.Remove(r.nodeID)
}

func (r *Runtime) leaveTargets() []string {
	// Esegue la logica di leave targets.
	targets := make([]string, 0)
	seen := make(map[string]struct{})

	for _, peer := range r.peerStore.List() {
		address := strings.TrimSpace(peer.GetGrpcAddress())
		if address == "" || peer.GetNodeId() == r.nodeID {
			continue
		}
		if _, exists := seen[address]; exists {
			continue
		}
		seen[address] = struct{}{}
		targets = append(targets, address)
	}

	for _, seedAddress := range r.seedPeers {
		address := strings.TrimSpace(seedAddress)
		if address == "" || address == r.advertiseAddress {
			continue
		}
		if _, exists := seen[address]; exists {
			continue
		}
		seen[address] = struct{}{}
		targets = append(targets, address)
	}

	return targets
}

func (r *Runtime) leaveCluster(address string, request *apiv1.JoinClusterRequest) error {
	// Esegue la logica di leave cluster.
	dialCtx, dialCancel := context.WithTimeout(context.Background(), r.dialTimeout)
	conn, err := grpc.DialContext(dialCtx, address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	dialCancel()
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			log.Printf("peer connection close error for %s: %v", address, closeErr)
		}
	}()

	callCtx, callCancel := context.WithTimeout(context.Background(), r.dialTimeout)
	defer callCancel()

	response := new(apiv1.GossipSyncResponse)
	if err := conn.Invoke(callCtx, registry.RegistryPeerControl_LeaveCluster_FullMethodName, request, response); err != nil {
		return err
	}
	if !response.GetAccepted() {
		return fmt.Errorf("leave rejected by %s", address)
	}
	return nil
}

func (r *Runtime) sendGossip(address string, request *apiv1.GossipSyncRequest) error {
	// Esegue la logica di send gossip.
	return r.withPeerClient(address, func(ctx context.Context, client apiv1.RegistryPeerClient) error {
		_, err := client.GossipSync(ctx, request)
		return err
	})
}

func (r *Runtime) pullState(address string, sinceUnix int64) error {
	// Esegue la logica di pull state.
	return r.withPeerClient(address, func(ctx context.Context, client apiv1.RegistryPeerClient) error {
		response, err := client.PullState(ctx, &apiv1.PullStateRequest{
			SourceNodeId: r.nodeID,
			SinceUnix:    sinceUnix,
		})
		if err != nil {
			return err
		}
		r.peerStore.MergeRemote(response.GetPeers())
		r.serviceStore.MergeRemote(response.GetRecords())
		return nil
	})
}

func (r *Runtime) withPeerClient(address string, fn func(ctx context.Context, client apiv1.RegistryPeerClient) error) error {
	// Esegue la logica di with peer client.
	dialCtx, dialCancel := context.WithTimeout(context.Background(), r.dialTimeout)
	conn, err := grpc.DialContext(dialCtx, address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	dialCancel()
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			log.Printf("peer connection close error for %s: %v", address, closeErr)
		}
	}()

	callCtx, callCancel := context.WithTimeout(context.Background(), r.dialTimeout)
	defer callCancel()

	client := apiv1.NewRegistryPeerClient(conn)
	return fn(callCtx, client)
}
