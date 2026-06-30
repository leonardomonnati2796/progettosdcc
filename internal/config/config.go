package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type RegistryConfig struct {
	Node    RegistryNodeConfig    `yaml:"node"`
	Cluster RegistryClusterConfig `yaml:"cluster"`
	Service RegistryServiceConfig `yaml:"service"`
}

type RegistryNodeConfig struct {
	ID               string `yaml:"id"`
	ListenAddress    string `yaml:"listen_address"`
	AdvertiseAddress string `yaml:"advertise_address"`
	StorageDir       string `yaml:"storage_dir"`
}

type RegistryClusterConfig struct {
	SeedPeers                []string `yaml:"seed_peers"`
	GossipIntervalSeconds    int      `yaml:"gossip_interval_seconds"`
	ReconcileIntervalSeconds int      `yaml:"reconcile_interval_seconds"`
	PeerTimeoutSeconds       int      `yaml:"peer_timeout_seconds"`
	MaxGossipFanout          int      `yaml:"max_gossip_fanout"`
}

type RegistryServiceConfig struct {
	HeartbeatTTLSeconds int `yaml:"heartbeat_ttl_seconds"`
}

type DemoServiceConfig struct {
	Service  DemoServiceIdentityConfig `yaml:"service"`
	Registry DemoServiceRegistryConfig `yaml:"registry"`
}

type DemoServiceIdentityConfig struct {
	ID         string `yaml:"id"`
	Name       string `yaml:"name"`
	Version    string `yaml:"version"`
	Endpoint   string `yaml:"endpoint"`
	HealthInit string `yaml:"health_init"`
}

type DemoServiceRegistryConfig struct {
	Endpoints                []string `yaml:"endpoints"`
	HeartbeatIntervalSeconds int      `yaml:"heartbeat_interval_seconds"`
}

func LoadRegistryConfig(path string) (*RegistryConfig, error) {
	var cfg RegistryConfig
	if err := readYAML(path, &cfg); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func LoadDemoServiceConfig(path string) (*DemoServiceConfig, error) {
	var cfg DemoServiceConfig
	if err := readYAML(path, &cfg); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *RegistryConfig) Validate() error {
	if c == nil {
		return errors.New("registry config is nil")
	}
	if strings.TrimSpace(c.Node.ID) == "" {
		return errors.New("node.id is required")
	}
	if strings.TrimSpace(c.Node.ListenAddress) == "" {
		return errors.New("node.listen_address is required")
	}
	if strings.TrimSpace(c.Node.AdvertiseAddress) == "" {
		return errors.New("node.advertise_address is required")
	}
	if c.Cluster.GossipIntervalSeconds <= 0 {
		return errors.New("cluster.gossip_interval_seconds must be > 0")
	}
	if c.Cluster.ReconcileIntervalSeconds <= 0 {
		return errors.New("cluster.reconcile_interval_seconds must be > 0")
	}
	if c.Cluster.PeerTimeoutSeconds <= 0 {
		return errors.New("cluster.peer_timeout_seconds must be > 0")
	}
	if c.Cluster.MaxGossipFanout <= 0 {
		return errors.New("cluster.max_gossip_fanout must be > 0")
	}
	if c.Service.HeartbeatTTLSeconds <= 0 {
		return errors.New("service.heartbeat_ttl_seconds must be > 0")
	}
	return nil
}

func (c *DemoServiceConfig) Validate() error {
	if c == nil {
		return errors.New("demo service config is nil")
	}
	if strings.TrimSpace(c.Service.ID) == "" {
		return errors.New("service.id is required")
	}
	if strings.TrimSpace(c.Service.Name) == "" {
		return errors.New("service.name is required")
	}
	if strings.TrimSpace(c.Service.Version) == "" {
		return errors.New("service.version is required")
	}
	if strings.TrimSpace(c.Service.Endpoint) == "" {
		return errors.New("service.endpoint is required")
	}
	if len(c.Registry.Endpoints) == 0 {
		return errors.New("registry.endpoints must include at least one endpoint")
	}
	for _, endpoint := range c.Registry.Endpoints {
		if strings.TrimSpace(endpoint) == "" {
			return errors.New("registry.endpoints cannot include empty values")
		}
	}
	if c.Registry.HeartbeatIntervalSeconds <= 0 {
		return errors.New("registry.heartbeat_interval_seconds must be > 0")
	}
	return nil
}

func readYAML(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config %q: %w", path, err)
	}
	if err := yaml.Unmarshal(data, out); err != nil {
		return fmt.Errorf("unmarshal config %q: %w", path, err)
	}
	return nil
}
