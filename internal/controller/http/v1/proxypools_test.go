package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"9router-gateway/internal/config"
	"9router-gateway/internal/database"
	"9router-gateway/internal/entity"
	"9router-gateway/internal/repository"
	"9router-gateway/internal/upstream"
)

func TestProxyPoolsPageAndAPI(t *testing.T) {
	tempDir := t.TempDir()
	db, err := database.InitDB(filepath.Join(tempDir, "test.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer db.Close()

	repo := repository.NewSQLiteRepo(db)
	cfg := &config.Config{
		SessionSecret: "test-secret-32-character-token-key",
	}

	h, err := NewHandler(cfg, repo, nil, nil)
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}

	mockPools := []upstream.ProxyPool{
		{
			ID:         "pp-1",
			IsActive:   true,
			TestStatus: "active",
			Data:       "http://user:pass@proxy1.example.com:8080",
			CreatedAt:  "2026-07-28 10:00:00",
			UpdatedAt:  "2026-07-28 10:00:00",
		},
		{
			ID:         "pp-2",
			IsActive:   true,
			TestStatus: "failed",
			Data:       "socks5://127.0.0.1:1080",
			CreatedAt:  "2026-07-28 11:00:00",
			UpdatedAt:  "2026-07-28 11:00:00",
		},
		{
			ID:         "pp-3",
			IsActive:   true,
			TestStatus: "",
			Data:       "http://10.0.0.2:3128",
			CreatedAt:  "2026-07-28 12:00:00",
			UpdatedAt:  "2026-07-28 12:00:00",
		},
	}

	mockCore := &upstream.MockCoreClient{
		GetProxyPoolsFunc: func(ctx context.Context) ([]upstream.ProxyPool, error) {
			return mockPools, nil
		},
		CreateProxyPoolFunc: func(ctx context.Context, payload map[string]interface{}) error {
			if payload == nil || payload["data"] == "" {
				return errors.New("invalid payload")
			}
			return nil
		},
		DeleteProxyPoolFunc: func(ctx context.Context, id string) error {
			if id == "not-found" {
				return errors.New("proxy not found")
			}
			return nil
		},
		TestProxyPoolFunc: func(ctx context.Context, id string) (map[string]interface{}, error) {
			if id == "pp-fail" {
				return nil, errors.New("connection refused")
			}
			return map[string]interface{}{"status": "active", "latencyMs": 42}, nil
		},
	}
	h.coreClient = mockCore

	adminUser := &entity.User{
		ID:       "admin-1",
		Username: "admin",
		Role:     "admin",
		IsActive: true,
	}

	ctx := context.WithValue(context.Background(), userContextKey, adminUser)

	t.Run("ProxyPoolsPage Render", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/proxy-pools", nil).WithContext(ctx)
		rr := httptest.NewRecorder()
		h.ProxyPoolsPage(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rr.Code)
		}

		body := rr.Body.String()
		if !strings.Contains(body, "Outbound Proxy Pools") {
			t.Errorf("expected body to contain 'Outbound Proxy Pools'")
		}
		if !strings.Contains(body, "proxy1.example.com:8080") {
			t.Errorf("expected body to contain proxy1 URL")
		}
		if !strings.Contains(body, "socks5://127.0.0.1:1080") {
			t.Errorf("expected body to contain socks5 URL")
		}
		if !strings.Contains(body, "Test All") {
			t.Errorf("expected body to contain 'Test All' button")
		}
		if !strings.Contains(body, "exportProxies") {
			t.Errorf("expected body to contain exportProxies script")
		}
	})

	t.Run("APIProxyPoolsCreate Success", func(t *testing.T) {
		payload := map[string]interface{}{
			"data":     "http://proxy3.example.com:8080",
			"isActive": 1,
		}
		b, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/api/proxy-pools/create", bytes.NewReader(b)).WithContext(ctx)
		rr := httptest.NewRecorder()
		h.APIProxyPoolsCreate(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rr.Code)
		}

		var resp map[string]bool
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil || !resp["success"] {
			t.Errorf("expected success: true in response")
		}
	})

	t.Run("APIProxyPoolsCreate Invalid Payload", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/proxy-pools/create", strings.NewReader("invalid-json")).WithContext(ctx)
		rr := httptest.NewRecorder()
		h.APIProxyPoolsCreate(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request, got %d", rr.Code)
		}
	})

	t.Run("APIProxyPoolsDelete Query Param", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/proxy-pools/delete?id=pp-1", nil).WithContext(ctx)
		rr := httptest.NewRecorder()
		h.APIProxyPoolsDelete(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rr.Code)
		}
	})

	t.Run("APIProxyPoolsDelete JSON Body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/proxy-pools/delete", strings.NewReader(`{"id":"pp-2"}`)).WithContext(ctx)
		rr := httptest.NewRecorder()
		h.APIProxyPoolsDelete(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rr.Code)
		}
	})

	t.Run("APIProxyPoolsDelete Missing ID", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/proxy-pools/delete", strings.NewReader(`{}`)).WithContext(ctx)
		rr := httptest.NewRecorder()
		h.APIProxyPoolsDelete(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request, got %d", rr.Code)
		}
	})

	t.Run("APIProxyPoolsTest Success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/proxy-pools/test?id=pp-1", nil).WithContext(ctx)
		rr := httptest.NewRecorder()
		h.APIProxyPoolsTest(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rr.Code)
		}

		var res map[string]interface{}
		if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil || res["status"] != "active" {
			t.Errorf("expected status 'active' in response, got %v", res)
		}
	})

	t.Run("APIProxyPoolsTest Failure", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/proxy-pools/test?id=pp-fail", nil).WithContext(ctx)
		rr := httptest.NewRecorder()
		h.APIProxyPoolsTest(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected 500 Internal Server Error, got %d", rr.Code)
		}
	})

	t.Run("APIProxyPoolsExport CSV", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/proxy-pools/export?format=csv", nil).WithContext(ctx)
		rr := httptest.NewRecorder()
		h.APIProxyPoolsExport(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rr.Code)
		}

		if !strings.Contains(rr.Header().Get("Content-Type"), "text/csv") {
			t.Errorf("expected text/csv Content-Type, got %s", rr.Header().Get("Content-Type"))
		}

		body := rr.Body.String()
		if !strings.Contains(body, "ID,Target,Protocol,TestStatus,IsActive,CreatedAt,UpdatedAt") {
			t.Errorf("expected CSV header row, got %s", body)
		}
		if !strings.Contains(body, "proxy1.example.com:8080") {
			t.Errorf("expected CSV body to contain proxy1 URL")
		}
		if !strings.Contains(body, "SOCKS5") {
			t.Errorf("expected CSV body to contain SOCKS5 protocol")
		}
	})

	t.Run("APIProxyPoolsExport JSON", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/proxy-pools/export?format=json", nil).WithContext(ctx)
		rr := httptest.NewRecorder()
		h.APIProxyPoolsExport(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rr.Code)
		}

		if !strings.Contains(rr.Header().Get("Content-Type"), "application/json") {
			t.Errorf("expected application/json Content-Type, got %s", rr.Header().Get("Content-Type"))
		}

		var items []map[string]interface{}
		if err := json.Unmarshal(rr.Body.Bytes(), &items); err != nil {
			t.Fatalf("failed to unmarshal exported JSON: %v", err)
		}

		if len(items) != 3 {
			t.Fatalf("expected 3 exported items, got %d", len(items))
		}
		if items[0]["target"] != "http://user:pass@proxy1.example.com:8080" {
			t.Errorf("expected item[0] target to match, got %v", items[0]["target"])
		}
		if items[1]["protocol"] != "SOCKS5" {
			t.Errorf("expected item[1] protocol SOCKS5, got %v", items[1]["protocol"])
		}
	})
}
