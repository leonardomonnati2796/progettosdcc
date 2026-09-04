package bootstrap

import (
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"google.golang.org/grpc"

	"github.com/leonardomonnati2796/distributed-service-registry/internal/config"
	"github.com/leonardomonnati2796/distributed-service-registry/internal/gossip"
	"github.com/leonardomonnati2796/distributed-service-registry/internal/registry"
	"github.com/leonardomonnati2796/distributed-service-registry/internal/storage"
	apiv1 "github.com/leonardomonnati2796/distributed-service-registry/pkg/api"
)

const DefaultConfigPath = "config/registry/example.yaml"

// Run assembles and runs one registry node until it receives a shutdown signal.
func Run(configPath string) {
	cfg, err := config.LoadRegistryConfig(configPath)
	if err != nil {
		log.Fatalf("cannot load registry config: %v", err)
	}

	listener, err := net.Listen("tcp", cfg.Node.ListenAddress)
	if err != nil {
		log.Fatalf("cannot listen on %s: %v", cfg.Node.ListenAddress, err)
	}

	store := storage.NewServiceStore()
	peerStore := storage.NewPeerStore()
	storageDir := strings.TrimSpace(cfg.Node.StorageDir)
	if storageDir != "" {
		snapshot, snapshotErr := storage.LoadSnapshot(storageDir)
		if snapshotErr != nil {
			log.Printf("snapshot load failed for %s, continuing with empty state: %v", storageDir, snapshotErr)
		} else {
			store.ReplaceAll(snapshot.Services)
			peerStore.ReplaceAll(snapshot.Peers)
			if len(snapshot.Services) > 0 || len(snapshot.Peers) > 0 {
				log.Printf("snapshot restored: services=%d peers=%d dir=%s", len(snapshot.Services), len(snapshot.Peers), storageDir)
			}
		}

		saveSnapshot := func() {
			if err := storage.SaveSnapshot(storageDir, store.ListForSync(), peerStore.List()); err != nil {
				log.Printf("snapshot save failed for %s: %v", storageDir, err)
			}
		}
		store.SetOnChange(saveSnapshot)
		peerStore.SetOnChange(saveSnapshot)
	}
	peerStore.UpsertSelf(cfg.Node.ID, cfg.Node.AdvertiseAddress, time.Now().Unix())

	serviceServer := registry.NewServiceRegistryServer(
		store,
		cfg.Node.ID,
		time.Duration(cfg.Service.HeartbeatTTLSeconds)*time.Second,
	)
	peerServer := registry.NewRegistryPeerServer(store, peerStore, cfg.Node.ID, cfg.Node.AdvertiseAddress)
	gossipRuntime := gossip.NewRuntime(cfg, store, peerStore)

	grpcServer := grpc.NewServer()
	apiv1.RegisterServiceRegistryServer(grpcServer, serviceServer)
	apiv1.RegisterRegistryPeerServer(grpcServer, peerServer)
	registry.RegisterRegistryPeerControlServer(grpcServer, peerServer)
	gossipRuntime.Start()

	staleTickerInterval := time.Duration(cfg.Service.HeartbeatTTLSeconds) * time.Second / 2
	if staleTickerInterval < time.Second {
		staleTickerInterval = time.Second
	}
	staleTicker := time.NewTicker(staleTickerInterval)
	staleStopCh := make(chan struct{})
	go func() {
		for {
			select {
			case <-staleTicker.C:
				store.MarkStale(time.Now().Unix(), int64(cfg.Service.HeartbeatTTLSeconds))
			case <-staleStopCh:
				staleTicker.Stop()
				return
			}
		}
	}()

	log.Printf(
		"registry node starting: node_id=%s listen=%s advertise=%s seed_peers=%d",
		cfg.Node.ID,
		cfg.Node.ListenAddress,
		cfg.Node.AdvertiseAddress,
		len(cfg.Cluster.SeedPeers),
	)

	serveErr := make(chan error, 1)
	go func() {
		if serveErrValue := grpcServer.Serve(listener); serveErrValue != nil {
			serveErr <- serveErrValue
		}
	}()

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serveErr:
		log.Printf("grpc server failed: %v", err)
		close(staleStopCh)
		gossipRuntime.Stop()
		os.Exit(1)
	case sig := <-signalCh:
		log.Printf("shutdown signal received: %s", sig.String())
		close(staleStopCh)
		gossipRuntime.Stop()
		gossipRuntime.GracefulLeave()
		grpcServer.GracefulStop()
		log.Printf("registry node stopped")
	}
}
