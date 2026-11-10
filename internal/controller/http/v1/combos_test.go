package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"9router-gateway/internal/config"
	"9router-gateway/internal/database"
	"9router-gateway/internal/repository"
	"9router-gateway/internal/upstream"
)

func TestAPICombosDuplicate(t *testing.T) {
	db, err := database.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer db.Close()

	repo := repository.NewSQLiteRepo(db)
	cfg := &config.Config{
		SessionSecret: "test-secret",
	}

	h, err := NewHandler(cfg, repo, nil, nil)
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}

	createdName := ""
	var createdModels []string

	mockCore := &upstream.MockCoreClient{
		GetCombosFunc: func(ctx context.Context) ([]upstream.Combo, error) {
			return []upstream.Combo{
				{
					ID:     "c-main",
					Name:   "main",
					Models: []string{"ag/gemini-3.7-flash-high", "ag/claude-sonnet-4-6"},
				},
				{
					ID:     "c-fast",
					Name:   "fast-free",
					Models: []string{"kr/glm-5", "oc/muse-spark"},
				},
			}, nil
		},
		CreateComboFunc: func(ctx context.Context, name string, models []string) error {
			createdName = name
			createdModels = models
			return nil
		},
	}
	h.coreClient = mockCore

	// 1. Missing combo id -> 400 Bad Request
	{
		reqBody := bytes.NewBufferString(`{"name":"custom-name"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/combos/duplicate", reqBody)
		rr := httptest.NewRecorder()
		h.APICombosDuplicate(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for missing id, got %d", rr.Code)
		}
	}

	// 2. Combo not found -> 404 Not Found
	{
		reqBody := bytes.NewBufferString(`{"id":"non-existent"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/combos/duplicate", reqBody)
		rr := httptest.NewRecorder()
		h.APICombosDuplicate(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for non-existent combo, got %d", rr.Code)
		}
	}

	// 3. Duplicate with custom name -> 200 OK + CreateCombo called with custom name
	{
		reqBody := bytes.NewBufferString(`{"id":"c-main","name":"main-v2"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/combos/duplicate", reqBody)
		rr := httptest.NewRecorder()
		h.APICombosDuplicate(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 for duplicate with custom name, got %d: %s", rr.Code, rr.Body.String())
		}
		if createdName != "main-v2" {
			t.Errorf("expected created combo name 'main-v2', got '%s'", createdName)
		}
		if len(createdModels) != 2 || createdModels[0] != "ag/gemini-3.7-flash-high" {
			t.Errorf("expected original models duplicated, got %v", createdModels)
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if resp["success"] != true || resp["name"] != "main-v2" {
			t.Errorf("unexpected response body: %v", resp)
		}
	}

	// 4. Duplicate with empty name -> defaults to <name>-copy
	{
		reqBody := bytes.NewBufferString(`{"id":"c-fast"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/combos/duplicate", reqBody)
		rr := httptest.NewRecorder()
		h.APICombosDuplicate(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 for duplicate with default name, got %d: %s", rr.Code, rr.Body.String())
		}
		if createdName != "fast-free-copy" {
			t.Errorf("expected created combo name 'fast-free-copy', got '%s'", createdName)
		}
		if len(createdModels) != 2 || createdModels[0] != "kr/glm-5" {
			t.Errorf("expected original models duplicated, got %v", createdModels)
		}
	}
}
