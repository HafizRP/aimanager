package upstream

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"9router-gateway/internal/config"
)

func TestDeriveCLIToken(t *testing.T) {
	tempDir := t.TempDir()

	// Prepare machine-id and auth/cli-secret
	machineID := "test-machine-id-123"
	cliSecret := "test-cli-secret-456"

	err := os.WriteFile(filepath.Join(tempDir, "machine-id"), []byte(machineID), 0600)
	if err != nil {
		t.Fatalf("failed to write machine-id: %v", err)
	}

	authDir := filepath.Join(tempDir, "auth")
	if err := os.MkdirAll(authDir, 0700); err != nil {
		t.Fatalf("failed to create auth dir: %v", err)
	}
	err = os.WriteFile(filepath.Join(authDir, "cli-secret"), []byte(cliSecret), 0600)
	if err != nil {
		t.Fatalf("failed to write cli-secret: %v", err)
	}

	cfg := &config.Config{
		NineRouterDataDir: tempDir,
	}

	token := DeriveCLIToken(cfg)
	if token == "" {
		t.Fatalf("expected non-empty token, got empty")
	}

	if len(token) > 16 {
		t.Errorf("expected token length <= 16, got %d", len(token))
	}

	// Verify deterministic output
	token2 := DeriveCLIToken(cfg)
	if token != token2 {
		t.Errorf("expected deterministic token %q, got %q", token, token2)
	}
}

func TestGetMergedModelsCache(t *testing.T) {
	client := &HTTPCoreClient{
		cachedObj: "list",
		cachedModels: []map[string]interface{}{
			{"id": "ag/gemini-3.7-flash-high"},
		},
		cachedModelsAt: time.Now(),
	}

	obj, data, err := client.GetMergedModels(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if obj != "list" || len(data) != 1 || data[0]["id"] != "ag/gemini-3.7-flash-high" {
		t.Fatalf("unexpected cached data: %v", data)
	}
}

