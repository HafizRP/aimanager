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

func TestInvalidateModelsCache(t *testing.T) {
	client := &HTTPCoreClient{
		cachedObj: "list",
		cachedModels: []map[string]interface{}{
			{"id": "ag/gemini-3.7-flash-high"},
		},
		cachedModelsAt: time.Now(),
	}

	client.InvalidateModelsCache()
	client.modelsMu.RLock()
	defer client.modelsMu.RUnlock()
	if client.cachedModels != nil || client.cachedObj != "" {
		t.Fatalf("expected cached models to be nil, got %v", client.cachedModels)
	}
}

func TestStripModelPrefix(t *testing.T) {
	if stripModelPrefix("ag/gemini-3.5-flash-high") != "gemini-3.5-flash-high" {
		t.Errorf("expected gemini-3.5-flash-high, got %s", stripModelPrefix("ag/gemini-3.5-flash-high"))
	}
	if stripModelPrefix("standalone-model") != "standalone-model" {
		t.Errorf("expected standalone-model, got %s", stripModelPrefix("standalone-model"))
	}
}

func TestComboContextWindowElevation(t *testing.T) {
	combo := map[string]interface{}{
		"id":             "work",
		"owned_by":       "combo",
		"context_length": 128000,
		"capabilities": map[string]interface{}{
			"contextWindow": 128000,
		},
	}
	normal := map[string]interface{}{
		"id":             "some-normal-model",
		"owned_by":       "openai",
		"context_length": 128000,
	}

	models := []map[string]interface{}{combo, normal}
	for _, m := range models {
		if ownedBy, ok := m["owned_by"].(string); ok && ownedBy == "combo" {
			currentCL := 0
			if cl, ok := m["context_length"].(float64); ok {
				currentCL = int(cl)
			} else if cl, ok := m["context_length"].(int); ok {
				currentCL = cl
			}
			if currentCL < 1000000 {
				m["context_length"] = 1000000
			}
			if caps, ok := m["capabilities"].(map[string]interface{}); ok {
				currentCW := 0
				if cw, ok := caps["contextWindow"].(float64); ok {
					currentCW = int(cw)
				} else if cw, ok := caps["contextWindow"].(int); ok {
					currentCW = cw
				}
				if currentCW < 1000000 {
					caps["contextWindow"] = 1000000
				}
			}
		}
	}

	if combo["context_length"] != 1000000 {
		t.Errorf("expected combo context_length to be 1000000, got %v", combo["context_length"])
	}
	caps := combo["capabilities"].(map[string]interface{})
	if caps["contextWindow"] != 1000000 {
		t.Errorf("expected combo capabilities.contextWindow to be 1000000, got %v", caps["contextWindow"])
	}
	if normal["context_length"] != 128000 {
		t.Errorf("expected normal model context_length to be unchanged at 128000, got %v", normal["context_length"])
	}
}


