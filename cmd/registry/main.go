package main

import (
	"flag"

	"github.com/leonardomonnati2796/distributed-service-registry/internal/bootstrap"
)

func main() {
	configPath := flag.String("config", bootstrap.DefaultConfigPath, "Path to registry node YAML config")
	flag.Parse()
	bootstrap.Run(*configPath)
}
