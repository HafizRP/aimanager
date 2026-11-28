package v1

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"9router-gateway/internal/config"
	"9router-gateway/internal/database"
	"9router-gateway/internal/entity"
	"9router-gateway/internal/repository"
)

func TestGetGatewayEndpoints(t *testing.T) {
	baseURL := "https://aimanager.example.com"
	upstreamURL := "http://127.0.0.1:20128"

	endpoints := getGatewayEndpoints(baseURL, upstreamURL)
	if len(endpoints) < 9 {
		t.Fatalf("expected at least 9 endpoints, got %d", len(endpoints))
	}

	hasChat := false
	hasModels := false
	hasHealth := false
	for _, ep := range endpoints {
		if ep.Name == "" || ep.Method == "" || ep.Path == "" || ep.URL == "" {
			t.Errorf("endpoint missing required fields: %+v", ep)
		}
		if ep.Path == "/v1/chat/completions" {
			hasChat = true
			if ep.Method != "POST" || ep.Category != "Inference" {
				t.Errorf("unexpected chat endpoint metadata: %+v", ep)
			}
		}
		if ep.Path == "/v1/models" {
			hasModels = true
			if ep.Method != "GET" || !ep.IsProbing {
				t.Errorf("expected models endpoint to be GET probeable: %+v", ep)
			}
		}
		if ep.Path == "/healthz" {
			hasHealth = true
			if ep.Method != "GET" || !ep.IsProbing || ep.Category != "Monitoring" {
				t.Errorf("unexpected healthz endpoint metadata: %+v", ep)
			}
		}
	}

	if !hasChat {
		t.Errorf("expected /v1/chat/completions in endpoint catalog")
	}
	if !hasModels {
		t.Errorf("expected /v1/models in endpoint catalog")
	}
	if !hasHealth {
		t.Errorf("expected /healthz in endpoint catalog")
	}
}

func TestEndpointPage(t *testing.T) {
	tempDir := t.TempDir()
	db, err := database.InitDB(filepath.Join(tempDir, "test_endpoint.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer db.Close()

	repo := repository.NewSQLiteRepo(db)
	cfg := &config.Config{
		SessionSecret: "test-secret-32-chars-long-123456",
		UpstreamURL:   "http://127.0.0.1:20128",
	}
	h, err := NewHandler(cfg, repo, nil, nil)
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}

	user := &entity.User{
		ID:            "u-ep-1",
		Username:      "endpointuser",
		Role:          "user",
		AllowedModels: `["*"]`,
		IsActive:      true,
	}

	req := httptest.NewRequest(http.MethodGet, "/endpoint", nil)
	ctx := context.WithValue(req.Context(), userContextKey, user)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.EndpointPage(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rr.Code)
	}

	body := rr.Body.String()
	if !strings.Contains(body, "Endpoint Hub") {
		t.Errorf("expected page to contain 'Endpoint Hub'")
	}
	if !strings.Contains(body, "/v1/chat/completions") {
		t.Errorf("expected page to contain '/v1/chat/completions'")
	}
	if !strings.Contains(body, "pingEndpoint") {
		t.Errorf("expected page to contain pingEndpoint JS function")
	}
	if !strings.Contains(body, "/api/endpoints/export?format=csv") {
		t.Errorf("expected page to contain export CSV link")
	}
}

func TestAPIEndpointsExport(t *testing.T) {
	tempDir := t.TempDir()
	db, err := database.InitDB(filepath.Join(tempDir, "test_ep_export.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer db.Close()

	repo := repository.NewSQLiteRepo(db)
	cfg := &config.Config{
		SessionSecret: "test-secret-32-chars-long-123456",
		UpstreamURL:   "http://127.0.0.1:20128",
	}
	h, err := NewHandler(cfg, repo, nil, nil)
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}

	user := &entity.User{
		ID:            "u-ep-2",
		Username:      "exportuser",
		Role:          "user",
		AllowedModels: `["*"]`,
		IsActive:      true,
	}
	authCtx := context.WithValue(context.Background(), userContextKey, user)

	// 1. CSV Export
	reqCSV := httptest.NewRequest(http.MethodGet, "/api/endpoints/export?format=csv", nil).WithContext(authCtx)
	rrCSV := httptest.NewRecorder()
	h.APIEndpointsExport(rrCSV, reqCSV)

	if rrCSV.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for CSV export, got %d", rrCSV.Code)
	}
	contentType := rrCSV.Header().Get("Content-Type")
	if !strings.Contains(contentType, "text/csv") {
		t.Errorf("expected text/csv Content-Type, got %s", contentType)
	}

	r := csv.NewReader(strings.NewReader(rrCSV.Body.String()))
	records, err := r.ReadAll()
	if err != nil {
		t.Fatalf("failed to parse CSV output: %v", err)
	}
	if len(records) < 10 { // 1 header + at least 9 endpoints
		t.Fatalf("expected at least 10 CSV rows, got %d", len(records))
	}
	if records[0][0] != "Name" || records[0][1] != "Method" {
		t.Errorf("unexpected CSV headers: %+v", records[0])
	}

	// 2. JSON Export
	reqJSON := httptest.NewRequest(http.MethodGet, "/api/endpoints/export?format=json", nil).WithContext(authCtx)
	rrJSON := httptest.NewRecorder()
	h.APIEndpointsExport(rrJSON, reqJSON)

	if rrJSON.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for JSON export, got %d", rrJSON.Code)
	}
	if !strings.Contains(rrJSON.Header().Get("Content-Type"), "application/json") {
		t.Errorf("expected application/json Content-Type, got %s", rrJSON.Header().Get("Content-Type"))
	}

	var exported []EndpointItem
	if err := json.Unmarshal(rrJSON.Body.Bytes(), &exported); err != nil {
		t.Fatalf("failed to unmarshal JSON export: %v", err)
	}
	if len(exported) < 9 {
		t.Fatalf("expected at least 9 exported items, got %d", len(exported))
	}
}
