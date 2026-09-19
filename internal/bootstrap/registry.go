package bootstrap

import (
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

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

	serviceServer := registry.NewServiceRegistryServer(
		store,
		cfg.Node.ID,
	)
	peerServer := registry.NewRegPeerServer(store, peerStore, cfg.Node.ID, cfg.Node.AdvertiseAddress)
	gossipRuntime := gossip.NewRuntime(cfg, store, peerStore)

	grpcServer := grpc.NewServer()
	apiv1.RegisterServiceRegistryServer(grpcServer, serviceServer)
	apiv1.RegisterRegPeerServer(grpcServer, peerServer)
	registry.RegisterRegPeerControlServer(grpcServer, peerServer)
	gossipRuntime.Start()

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
		gossipRuntime.Stop()
		os.Exit(1)
	case sig := <-signalCh:
		log.Printf("shutdown signal received: %s", sig.String())
		gossipRuntime.Stop()
		gossipRuntime.GracefulLeave()
		grpcServer.GracefulStop()
		log.Printf("registry node stopped")
	}
}
