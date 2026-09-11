// Package upstream provides a client for the 9router Core management API.
package upstream

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"9router-gateway/internal/config"
)

// CoreClient talks to the 9router Core HTTP API.
type CoreClient struct {
	cfg        *config.Config
	httpClient *http.Client
	mu         sync.RWMutex
	cliToken   string
}

// NewCoreClient creates a CoreClient targeting the configured upstream.
func NewCoreClient(cfg *config.Config) *CoreClient {
	return &CoreClient{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (c *CoreClient) deriveCLIToken() string {
	c.mu.RLock()
	if c.cliToken != "" {
		token := c.cliToken
		c.mu.RUnlock()
		return token
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cliToken != "" {
		return c.cliToken
	}

	c.cliToken = DeriveCLIToken(c.cfg)
	return c.cliToken
}

// ResetCLIToken invalidates the cached CLI token so it can be re-derived on next request.
func (c *CoreClient) ResetCLIToken() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cliToken = ""
}

// DeriveCLIToken discovers machine-id and auth/cli-secret from core directories
// configured in Config (via .env or derived from database paths) and computes the 9router CLI auth token.
func DeriveCLIToken(cfg *config.Config) string {
	dirs := cfg.GetCoreDataDirs()
	var machineIDBytes, cliSecretBytes []byte

	for _, dir := range dirs {
		if len(machineIDBytes) == 0 {
			if b, err := os.ReadFile(filepath.Join(dir, "machine-id")); err == nil {
				machineIDBytes = b
			}
		}
		if len(cliSecretBytes) == 0 {
			if b, err := os.ReadFile(filepath.Join(dir, "auth", "cli-secret")); err == nil {
				cliSecretBytes = b
			}
		}
		if len(machineIDBytes) > 0 && len(cliSecretBytes) > 0 {
			break
		}
	}

	machineID := strings.TrimSpace(string(machineIDBytes))
	cliSecret := strings.TrimSpace(string(cliSecretBytes))
	if machineID == "" || cliSecret == "" {
		return ""
	}

	h := sha256.Sum256([]byte(machineID + "9r-cli-auth" + cliSecret))
	token := hex.EncodeToString(h[:])
	if len(token) > 16 {
		token = token[:16]
	}
	return token
}

func (c *CoreClient) doRequest(ctx context.Context, method, endpoint string, reqBody interface{}) ([]byte, error) {
	url := fmt.Sprintf("%s%s", strings.TrimRight(c.cfg.GetUpstreamURL(), "/"), endpoint)

	var bodyReader io.Reader
	if reqBody != nil {
		b, err := json.Marshal(reqBody)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	cliToken := c.deriveCLIToken()
	if cliToken != "" {
		req.Header.Set("x-9r-cli-token", cliToken)
	}
	if apiKey := c.cfg.GetUpstreamAPIKey(); apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("core request failed: %w", err)
	}
	defer resp.Body.Close()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return respData, fmt.Errorf("upstream returned %d: %s", resp.StatusCode, string(respData))
	}

	return respData, nil
}

// -------------------------------------------------------------
// Providers Management
// -------------------------------------------------------------

// ProviderConnection describes one configured upstream provider account.
type ProviderConnection struct {
	ID                   string                 `json:"id"`
	Provider             string                 `json:"provider"`
	AuthType             string                 `json:"authType"`
	Name                 string                 `json:"name"`
	Email                string                 `json:"email"`
	Priority             int                    `json:"priority"`
	IsActive             bool                   `json:"isActive"`
	CreatedAt            string                 `json:"createdAt"`
	UpdatedAt            string                 `json:"updatedAt"`
	TestStatus           string                 `json:"testStatus,omitempty"`
	LastError            string                 `json:"lastError,omitempty"`
	LastErrorAt          string                 `json:"lastErrorAt,omitempty"`
	ProviderSpecificData map[string]interface{} `json:"providerSpecificData,omitempty"`
}

// GetProviders lists all configured provider connections.
func (c *CoreClient) GetProviders(ctx context.Context) ([]ProviderConnection, error) {
	data, err := c.doRequest(ctx, http.MethodGet, "/api/providers", nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		Connections []ProviderConnection `json:"connections"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result.Connections, nil
}

// ToggleProvider activates or deactivates a provider connection.
func (c *CoreClient) ToggleProvider(ctx context.Context, id string, isActive bool) error {
	payload := map[string]interface{}{
		"isActive": isActive,
	}
	_, err := c.doRequest(ctx, http.MethodPut, fmt.Sprintf("/api/providers/%s", id), payload)
	return err
}

// SetProviderPriority updates the routing priority of a provider connection.
func (c *CoreClient) SetProviderPriority(ctx context.Context, id string, priority int) error {
	payload := map[string]interface{}{
		"priority": priority,
	}
	_, err := c.doRequest(ctx, http.MethodPut, fmt.Sprintf("/api/providers/%s", id), payload)
	return err
}

// TestProvider asks 9router Core to test connectivity for a provider connection.
func (c *CoreClient) TestProvider(ctx context.Context, id string) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodPost, fmt.Sprintf("/api/providers/%s/test", id), nil)
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// DeleteProvider removes a provider connection by ID.
func (c *CoreClient) DeleteProvider(ctx context.Context, id string) error {
	_, err := c.doRequest(ctx, http.MethodDelete, fmt.Sprintf("/api/providers/%s", id), nil)
	return err
}

// CreateProvider creates a new provider connection and returns its data.
func (c *CoreClient) CreateProvider(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodPost, "/api/providers", payload)
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// -------------------------------------------------------------
// Combos Management
// -------------------------------------------------------------

// Combo is a named group of models routable through the upstream.
type Combo struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Kind      *string  `json:"kind"`
	Models    []string `json:"models"`
	CreatedAt string   `json:"createdAt"`
	UpdatedAt string   `json:"updatedAt"`
}

// GetCombos lists all model combos.
func (c *CoreClient) GetCombos(ctx context.Context) ([]Combo, error) {
	data, err := c.doRequest(ctx, http.MethodGet, "/api/combos", nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		Combos []Combo `json:"combos"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result.Combos, nil
}

// CreateCombo creates a new combo with the given name and model list.
func (c *CoreClient) CreateCombo(ctx context.Context, name string, models []string) error {
	payload := map[string]interface{}{
		"name":   name,
		"models": models,
	}
	_, err := c.doRequest(ctx, http.MethodPost, "/api/combos", payload)
	return err
}

// UpdateCombo updates the name and model list of an existing combo.
func (c *CoreClient) UpdateCombo(ctx context.Context, id string, name string, models []string) error {
	payload := map[string]interface{}{
		"name":   name,
		"models": models,
	}
	_, err := c.doRequest(ctx, http.MethodPut, fmt.Sprintf("/api/combos/%s", id), payload)
	return err
}

// DeleteCombo removes a combo by ID.
func (c *CoreClient) DeleteCombo(ctx context.Context, id string) error {
	_, err := c.doRequest(ctx, http.MethodDelete, fmt.Sprintf("/api/combos/%s", id), nil)
	return err
}

// -------------------------------------------------------------
// Settings & Token Saver
// -------------------------------------------------------------

// GetSettings retrieves the current 9router Core settings.
func (c *CoreClient) GetSettings(ctx context.Context) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodGet, "/api/settings", nil)
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, err
	}
	return res, nil
}

// UpdateSettings applies partial updates to 9router Core settings.
// Core accepts PATCH (not POST) on /api/settings.
func (c *CoreClient) UpdateSettings(ctx context.Context, updates map[string]interface{}) error {
	_, err := c.doRequest(ctx, http.MethodPatch, "/api/settings", updates)
	return err
}

// -------------------------------------------------------------
// Model Aliases
// -------------------------------------------------------------

// GetModelAliases returns all model aliases as alias-to-model pairs.
func (c *CoreClient) GetModelAliases(ctx context.Context) (map[string]string, error) {
	data, err := c.doRequest(ctx, http.MethodGet, "/api/models/alias", nil)
	if err != nil {
		return nil, err
	}
	var res struct {
		Aliases map[string]string `json:"aliases"`
	}
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, err
	}
	return res.Aliases, nil
}

// SetModelAlias maps an alias to a model.
func (c *CoreClient) SetModelAlias(ctx context.Context, alias, model string) error {
	payload := map[string]string{
		"alias": alias,
		"model": model,
	}
	_, err := c.doRequest(ctx, http.MethodPut, "/api/models/alias", payload)
	return err
}

// DeleteModelAlias removes an alias mapping.
func (c *CoreClient) DeleteModelAlias(ctx context.Context, alias string) error {
	ep := fmt.Sprintf("/api/models/alias?alias=%s", strings.TrimSpace(alias))
	_, err := c.doRequest(ctx, http.MethodDelete, ep, nil)
	return err
}

// -------------------------------------------------------------
// Proxy Pools
// -------------------------------------------------------------

// ProxyPool describes one configured upstream proxy pool.
type ProxyPool struct {
	ID         string                 `json:"id"`
	IsActive   bool                   `json:"isActive"`
	TestStatus string                 `json:"testStatus"`
	Data       map[string]interface{} `json:"data"`
	CreatedAt  string                 `json:"createdAt"`
	UpdatedAt  string                 `json:"updatedAt"`
}

// GetProxyPools lists all configured proxy pools.
func (c *CoreClient) GetProxyPools(ctx context.Context) ([]ProxyPool, error) {
	data, err := c.doRequest(ctx, http.MethodGet, "/api/proxy-pools", nil)
	if err != nil {
		return nil, err
	}
	var res struct {
		ProxyPools []ProxyPool `json:"proxyPools"`
	}
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, err
	}
	return res.ProxyPools, nil
}

// CreateProxyPool creates a new proxy pool.
func (c *CoreClient) CreateProxyPool(ctx context.Context, payload map[string]interface{}) error {
	_, err := c.doRequest(ctx, http.MethodPost, "/api/proxy-pools", payload)
	return err
}

// DeleteProxyPool removes a proxy pool by ID.
func (c *CoreClient) DeleteProxyPool(ctx context.Context, id string) error {
	_, err := c.doRequest(ctx, http.MethodDelete, fmt.Sprintf("/api/proxy-pools/%s", id), nil)
	return err
}

// TestProxyPool asks 9router Core to test connectivity for a proxy pool.
func (c *CoreClient) TestProxyPool(ctx context.Context, id string) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodPost, fmt.Sprintf("/api/proxy-pools/%s/test", id), nil)
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// -------------------------------------------------------------
// Service operations (Headroom, PXPipe, Translator)
// -------------------------------------------------------------

// ServiceStatus proxies GET /api/{service}/status from 9router Core.
func (c *CoreClient) ServiceStatus(ctx context.Context, service string) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodGet, fmt.Sprintf("/api/%s/status", service), nil)
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// ServiceStats proxies GET /api/{service}/stats from 9router Core.
func (c *CoreClient) ServiceStats(ctx context.Context, service string) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodGet, fmt.Sprintf("/api/%s/stats", service), nil)
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// ServiceAction proxies POST /api/{service}/{action} to 9router Core.
func (c *CoreClient) ServiceAction(ctx context.Context, service, action string, payload map[string]interface{}) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodPost, fmt.Sprintf("/api/%s/%s", service, action), payload)
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// TranslatorTranslate proxies POST /api/translator/translate to 9router Core.
func (c *CoreClient) TranslatorTranslate(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodPost, "/api/translator/translate", payload)
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// -------------------------------------------------------------
// Read-only views (Usage analytics, Console logs)
// -------------------------------------------------------------

// UsageStats proxies GET /api/usage/stats from 9router Core.
func (c *CoreClient) UsageStats(ctx context.Context) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodGet, "/api/usage/stats", nil)
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// ConsoleLogs proxies GET /api/translator/console-logs from 9router Core.
func (c *CoreClient) ConsoleLogs(ctx context.Context) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodGet, "/api/translator/console-logs", nil)
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// -------------------------------------------------------------
// Control plane (Provider Nodes, MITM Bridge, MCP Inspector)
// -------------------------------------------------------------

// ProviderNode is a self-hosted OpenAI-compatible endpoint in 9router Core.
type ProviderNode struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Name      string `json:"name"`
	Prefix    string `json:"prefix"`
	APIType   string `json:"apiType"`
	BaseURL   string `json:"baseUrl"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// GetProviderNodes lists self-hosted provider nodes.
func (c *CoreClient) GetProviderNodes(ctx context.Context) ([]ProviderNode, error) {
	data, err := c.doRequest(ctx, http.MethodGet, "/api/provider-nodes", nil)
	if err != nil {
		return nil, err
	}
	var res struct {
		Nodes []ProviderNode `json:"nodes"`
	}
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, err
	}
	return res.Nodes, nil
}

// CreateProviderNode creates a self-hosted provider node.
func (c *CoreClient) CreateProviderNode(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodPost, "/api/provider-nodes", payload)
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// UpdateProviderNode replaces a provider node (core requires full fields).
func (c *CoreClient) UpdateProviderNode(ctx context.Context, id string, payload map[string]interface{}) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodPut, fmt.Sprintf("/api/provider-nodes/%s", id), payload)
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// DeleteProviderNode removes a provider node.
func (c *CoreClient) DeleteProviderNode(ctx context.Context, id string) error {
	_, err := c.doRequest(ctx, http.MethodDelete, fmt.Sprintf("/api/provider-nodes/%s", id), nil)
	return err
}

// ValidateProviderNode dry-runs a node definition (requires name/prefix/baseUrl/apiKey).
func (c *CoreClient) ValidateProviderNode(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodPost, "/api/provider-nodes/validate", payload)
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// MitmStatus proxies GET /api/cli-tools/antigravity-mitm from 9router Core.
func (c *CoreClient) MitmStatus(ctx context.Context) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodGet, "/api/cli-tools/antigravity-mitm", nil)
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// MitmStart starts the MITM bridge server (core runs as root: no sudo needed).
func (c *CoreClient) MitmStart(ctx context.Context, apiKey string) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodPost, "/api/cli-tools/antigravity-mitm", map[string]interface{}{"apiKey": apiKey})
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// MitmStop stops the MITM bridge server.
func (c *CoreClient) MitmStop(ctx context.Context) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodDelete, "/api/cli-tools/antigravity-mitm", map[string]interface{}{})
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// MitmDNS toggles per-tool DNS interception (tool: antigravity/kiro/copilot/cursor,
// action: enable/disable). trust-cert is deliberately NOT proxied (host trust store).
func (c *CoreClient) MitmDNS(ctx context.Context, tool, action string) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodPatch, "/api/cli-tools/antigravity-mitm", map[string]interface{}{"tool": tool, "action": action})
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// MitmAliasGet returns model alias mappings for a tool.
func (c *CoreClient) MitmAliasGet(ctx context.Context, tool string) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodGet, fmt.Sprintf("/api/cli-tools/antigravity-mitm/alias?tool=%s", tool), nil)
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// MitmAliasPut saves model alias mappings for a tool.
func (c *CoreClient) MitmAliasPut(ctx context.Context, tool string, mappings map[string]interface{}) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodPut, "/api/cli-tools/antigravity-mitm/alias", map[string]interface{}{"tool": tool, "mappings": mappings})
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// MCPRegistry proxies the cowork MCP server directory from 9router Core.
func (c *CoreClient) MCPRegistry(ctx context.Context) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodGet, "/api/cli-tools/cowork-mcp-registry", nil)
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// MCPInspect asks 9router Core to list tools of an MCP server URL (SSRF-guarded in core).
func (c *CoreClient) MCPInspect(ctx context.Context, url string) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodPost, "/api/cli-tools/cowork-mcp-tools", map[string]interface{}{"url": url})
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}
