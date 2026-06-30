package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	apiv1 "github.com/leonardomonnati2796/distributed-service-registry/pkg/api"
)

const snapshotFileName = "registry_snapshot.json"

type Snapshot struct {
	Services []*apiv1.ServiceRecord `json:"services"`
	Peers    []*apiv1.NodeInfo      `json:"peers"`
}

func LoadSnapshot(storageDir string) (*Snapshot, error) {
	dir := strings.TrimSpace(storageDir)
	if dir == "" {
		return &Snapshot{}, nil
	}

	path := filepath.Join(dir, snapshotFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Snapshot{}, nil
		}
		return nil, fmt.Errorf("read snapshot %q: %w", path, err)
	}

	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("unmarshal snapshot %q: %w", path, err)
	}
	if snapshot.Services == nil {
		snapshot.Services = make([]*apiv1.ServiceRecord, 0)
	}
	if snapshot.Peers == nil {
		snapshot.Peers = make([]*apiv1.NodeInfo, 0)
	}
	return &snapshot, nil
}

func SaveSnapshot(storageDir string, services []*apiv1.ServiceRecord, peers []*apiv1.NodeInfo) error {
	dir := strings.TrimSpace(storageDir)
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create snapshot directory %q: %w", dir, err)
	}

	snapshot := &Snapshot{Services: services, Peers: peers}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}

	finalPath := filepath.Join(dir, snapshotFileName)
	tmpPath := filepath.Join(dir, fmt.Sprintf("%s.tmp-%d", snapshotFileName, time.Now().UnixNano()))
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("write snapshot tmp %q: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("commit snapshot %q: %w", finalPath, err)
	}
	return nil
}

