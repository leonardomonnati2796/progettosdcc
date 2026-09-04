package bootstrap

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfigPathPointsToRegistryConfiguration(t *testing.T) {
	root := filepath.Join("..", "..")
	configPath := filepath.Join(root, DefaultConfigPath)

	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("default config path %q is not available: %v", configPath, err)
	}
}
