// Package upstream provides a client for the 9router Core management API.
package upstream

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"9router-gateway/internal/config"

	_ "modernc.org/sqlite"
)

// CoreClient defines the client interface for the 9router Core HTTP and management API.
type CoreClient interface {
	ResetCLIToken()
	GetProviders(ctx context.Context) ([]ProviderConnection, error)
	ToggleProvider(ctx context.Context, id string, isActive bool) error
	SetProviderPriority(ctx context.Context, id string, priority int) error
	TestProvider(ctx context.Context, id string) (map[string]interface{}, error)
	DeleteProvider(ctx context.Context, id string) error
	CreateProvider(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error)
	GetCombos(ctx context.Context) ([]Combo, error)
	CreateCombo(ctx context.Context, name string, models []string) error
	UpdateCombo(ctx context.Context, id string, name string, models []string) error
	DeleteCombo(ctx context.Context, id string) error
	GetSettings(ctx context.Context) (map[string]interface{}, error)
	UpdateSettings(ctx context.Context, updates map[string]interface{}) error
	GetModelAliases(ctx context.Context) (map[string]string, error)
	SetModelAlias(ctx context.Context, alias, model string) error
	DeleteModelAlias(ctx context.Context, alias string) error
	GetProxyPools(ctx context.Context) ([]ProxyPool, error)
	CreateProxyPool(ctx context.Context, payload map[string]interface{}) error
	DeleteProxyPool(ctx context.Context, id string) error
	TestProxyPool(ctx context.Context, id string) (map[string]interface{}, error)
	ServiceStatus(ctx context.Context, service string) (map[string]interface{}, error)
	ServiceStats(ctx context.Context, service string) (map[string]interface{}, error)
	ServiceAction(ctx context.Context, service, action string, payload map[string]interface{}) (map[string]interface{}, error)
	TranslatorTranslate(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error)
	UsageStats(ctx context.Context) (map[string]interface{}, error)
	ConsoleLogs(ctx context.Context) (map[string]interface{}, error)
	GetProviderNodes(ctx context.Context) ([]ProviderNode, error)
	CreateProviderNode(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error)
	UpdateProviderNode(ctx context.Context, id string, payload map[string]interface{}) (map[string]interface{}, error)
	DeleteProviderNode(ctx context.Context, id string) error
	ValidateProviderNode(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error)
	MitmStatus(ctx context.Context) (map[string]interface{}, error)
	MitmStart(ctx context.Context, apiKey string) (map[string]interface{}, error)
	MitmStop(ctx context.Context) (map[string]interface{}, error)
	MitmDNS(ctx context.Context, tool, action string) (map[string]interface{}, error)
	MitmAliasGet(ctx context.Context, tool string) (map[string]interface{}, error)
	MitmAliasPut(ctx context.Context, tool string, mappings map[string]interface{}) (map[string]interface{}, error)
	MCPRegistry(ctx context.Context) (map[string]interface{}, error)
	MCPInspect(ctx context.Context, url string) (map[string]interface{}, error)
	OAuthAuthorize(ctx context.Context, provider, redirectURI string) (map[string]interface{}, error)
	OAuthExchange(ctx context.Context, provider string, payload map[string]interface{}) (map[string]interface{}, error)
	KiroSocialAuthorize(ctx context.Context, idp, redirectURI string) (map[string]interface{}, error)
	KiroSocialExchange(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error)
	MediaVoices(ctx context.Context, engine string) (map[string]interface{}, error)
	PxpipeLogs(ctx context.Context) (map[string]interface{}, error)
	PxpipeHealth(ctx context.Context) (map[string]interface{}, error)
	TranslatorSend(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error)
	CLIToolSettings(ctx context.Context, tool string) (map[string]interface{}, error)
	Pricing(ctx context.Context) (map[string]interface{}, error)
	CoreVersion(ctx context.Context) (map[string]interface{}, error)
	CoreKeys(ctx context.Context) (map[string]interface{}, error)
	GetMergedModels(ctx context.Context) (string, []map[string]interface{}, error)
}

// HTTPCoreClient talks to the 9router Core HTTP API.
type HTTPCoreClient struct {
	cfg        *config.Config
	httpClient *http.Client
	mu         sync.RWMutex
	cliToken   string

	modelsMu       sync.RWMutex
	cachedObj      string
	cachedModels   []map[string]interface{}
	cachedModelsAt time.Time
	modelsFetching bool
}

// Ensure HTTPCoreClient implements CoreClient.
var _ CoreClient = (*HTTPCoreClient)(nil)

// DefaultCoreClient is an alias to HTTPCoreClient.
type DefaultCoreClient = HTTPCoreClient

// NewCoreClient creates a CoreClient targeting the configured upstream.
func NewCoreClient(cfg *config.Config) CoreClient {
	return NewHTTPCoreClient(cfg)
}

// NewHTTPCoreClient creates a concrete HTTPCoreClient targeting the configured upstream.
func NewHTTPCoreClient(cfg *config.Config) *HTTPCoreClient {
	return &HTTPCoreClient{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (c *HTTPCoreClient) deriveCLIToken() string {
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
func (c *HTTPCoreClient) ResetCLIToken() {
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

func (c *HTTPCoreClient) doRequest(ctx context.Context, method, endpoint string, reqBody interface{}) ([]byte, error) {
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
func (c *HTTPCoreClient) GetProviders(ctx context.Context) ([]ProviderConnection, error) {
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
func (c *HTTPCoreClient) ToggleProvider(ctx context.Context, id string, isActive bool) error {
	payload := map[string]interface{}{
		"isActive": isActive,
	}
	_, err := c.doRequest(ctx, http.MethodPut, fmt.Sprintf("/api/providers/%s", id), payload)
	return err
}

// SetProviderPriority updates the routing priority of a provider connection.
func (c *HTTPCoreClient) SetProviderPriority(ctx context.Context, id string, priority int) error {
	payload := map[string]interface{}{
		"priority": priority,
	}
	_, err := c.doRequest(ctx, http.MethodPut, fmt.Sprintf("/api/providers/%s", id), payload)
	return err
}

// TestProvider asks 9router Core to test connectivity for a provider connection.
func (c *HTTPCoreClient) TestProvider(ctx context.Context, id string) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodPost, fmt.Sprintf("/api/providers/%s/test", id), nil)
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// DeleteProvider removes a provider connection by ID.
func (c *HTTPCoreClient) DeleteProvider(ctx context.Context, id string) error {
	_, err := c.doRequest(ctx, http.MethodDelete, fmt.Sprintf("/api/providers/%s", id), nil)
	return err
}

// CreateProvider creates a new provider connection and returns its data.
func (c *HTTPCoreClient) CreateProvider(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
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
func (c *HTTPCoreClient) GetCombos(ctx context.Context) ([]Combo, error) {
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
func (c *HTTPCoreClient) CreateCombo(ctx context.Context, name string, models []string) error {
	payload := map[string]interface{}{
		"name":   name,
		"models": models,
	}
	_, err := c.doRequest(ctx, http.MethodPost, "/api/combos", payload)
	return err
}

// UpdateCombo updates the name and model list of an existing combo.
func (c *HTTPCoreClient) UpdateCombo(ctx context.Context, id string, name string, models []string) error {
	payload := map[string]interface{}{
		"name":   name,
		"models": models,
	}
	_, err := c.doRequest(ctx, http.MethodPut, fmt.Sprintf("/api/combos/%s", id), payload)
	return err
}

// DeleteCombo removes a combo by ID.
func (c *HTTPCoreClient) DeleteCombo(ctx context.Context, id string) error {
	_, err := c.doRequest(ctx, http.MethodDelete, fmt.Sprintf("/api/combos/%s", id), nil)
	return err
}

// -------------------------------------------------------------
// Settings & Token Saver
// -------------------------------------------------------------

// GetSettings retrieves the current 9router Core settings.
func (c *HTTPCoreClient) GetSettings(ctx context.Context) (map[string]interface{}, error) {
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
func (c *HTTPCoreClient) UpdateSettings(ctx context.Context, updates map[string]interface{}) error {
	_, err := c.doRequest(ctx, http.MethodPatch, "/api/settings", updates)
	return err
}

// -------------------------------------------------------------
// Model Aliases
// -------------------------------------------------------------

// GetModelAliases returns all model aliases as alias-to-model pairs.
func (c *HTTPCoreClient) GetModelAliases(ctx context.Context) (map[string]string, error) {
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
func (c *HTTPCoreClient) SetModelAlias(ctx context.Context, alias, model string) error {
	payload := map[string]string{
		"alias": alias,
		"model": model,
	}
	_, err := c.doRequest(ctx, http.MethodPut, "/api/models/alias", payload)
	return err
}

// DeleteModelAlias removes an alias mapping.
func (c *HTTPCoreClient) DeleteModelAlias(ctx context.Context, alias string) error {
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
func (c *HTTPCoreClient) GetProxyPools(ctx context.Context) ([]ProxyPool, error) {
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
func (c *HTTPCoreClient) CreateProxyPool(ctx context.Context, payload map[string]interface{}) error {
	_, err := c.doRequest(ctx, http.MethodPost, "/api/proxy-pools", payload)
	return err
}

// DeleteProxyPool removes a proxy pool by ID.
func (c *HTTPCoreClient) DeleteProxyPool(ctx context.Context, id string) error {
	_, err := c.doRequest(ctx, http.MethodDelete, fmt.Sprintf("/api/proxy-pools/%s", id), nil)
	return err
}

// TestProxyPool asks 9router Core to test connectivity for a proxy pool.
func (c *HTTPCoreClient) TestProxyPool(ctx context.Context, id string) (map[string]interface{}, error) {
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
func (c *HTTPCoreClient) ServiceStatus(ctx context.Context, service string) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodGet, fmt.Sprintf("/api/%s/status", service), nil)
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// ServiceStats proxies GET /api/{service}/stats from 9router Core.
func (c *HTTPCoreClient) ServiceStats(ctx context.Context, service string) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodGet, fmt.Sprintf("/api/%s/stats", service), nil)
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// ServiceAction proxies POST /api/{service}/{action} to 9router Core.
func (c *HTTPCoreClient) ServiceAction(ctx context.Context, service, action string, payload map[string]interface{}) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodPost, fmt.Sprintf("/api/%s/%s", service, action), payload)
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// TranslatorTranslate proxies POST /api/translator/translate to 9router Core.
func (c *HTTPCoreClient) TranslatorTranslate(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
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
func (c *HTTPCoreClient) UsageStats(ctx context.Context) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodGet, "/api/usage/stats", nil)
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// ConsoleLogs proxies GET /api/translator/console-logs from 9router Core.
func (c *HTTPCoreClient) ConsoleLogs(ctx context.Context) (map[string]interface{}, error) {
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
func (c *HTTPCoreClient) GetProviderNodes(ctx context.Context) ([]ProviderNode, error) {
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
func (c *HTTPCoreClient) CreateProviderNode(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodPost, "/api/provider-nodes", payload)
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// UpdateProviderNode replaces a provider node (core requires full fields).
func (c *HTTPCoreClient) UpdateProviderNode(ctx context.Context, id string, payload map[string]interface{}) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodPut, fmt.Sprintf("/api/provider-nodes/%s", id), payload)
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// DeleteProviderNode removes a provider node.
func (c *HTTPCoreClient) DeleteProviderNode(ctx context.Context, id string) error {
	_, err := c.doRequest(ctx, http.MethodDelete, fmt.Sprintf("/api/provider-nodes/%s", id), nil)
	return err
}

// ValidateProviderNode dry-runs a node definition (requires name/prefix/baseUrl/apiKey).
func (c *HTTPCoreClient) ValidateProviderNode(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodPost, "/api/provider-nodes/validate", payload)
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// MitmStatus proxies GET /api/cli-tools/antigravity-mitm from 9router Core.
func (c *HTTPCoreClient) MitmStatus(ctx context.Context) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodGet, "/api/cli-tools/antigravity-mitm", nil)
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// MitmStart starts the MITM bridge server (core runs as root: no sudo needed).
func (c *HTTPCoreClient) MitmStart(ctx context.Context, apiKey string) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodPost, "/api/cli-tools/antigravity-mitm", map[string]interface{}{"apiKey": apiKey})
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// MitmStop stops the MITM bridge server.
func (c *HTTPCoreClient) MitmStop(ctx context.Context) (map[string]interface{}, error) {
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
func (c *HTTPCoreClient) MitmDNS(ctx context.Context, tool, action string) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodPatch, "/api/cli-tools/antigravity-mitm", map[string]interface{}{"tool": tool, "action": action})
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// MitmAliasGet returns model alias mappings for a tool.
func (c *HTTPCoreClient) MitmAliasGet(ctx context.Context, tool string) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodGet, fmt.Sprintf("/api/cli-tools/antigravity-mitm/alias?tool=%s", tool), nil)
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// MitmAliasPut saves model alias mappings for a tool.
func (c *HTTPCoreClient) MitmAliasPut(ctx context.Context, tool string, mappings map[string]interface{}) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodPut, "/api/cli-tools/antigravity-mitm/alias", map[string]interface{}{"tool": tool, "mappings": mappings})
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// MCPRegistry proxies the cowork MCP server directory from 9router Core.
func (c *HTTPCoreClient) MCPRegistry(ctx context.Context) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodGet, "/api/cli-tools/cowork-mcp-registry", nil)
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// MCPInspect asks 9router Core to list tools of an MCP server URL (SSRF-guarded in core).
func (c *HTTPCoreClient) MCPInspect(ctx context.Context, url string) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodPost, "/api/cli-tools/cowork-mcp-tools", map[string]interface{}{"url": url})
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// -------------------------------------------------------------
// Parity rest (OAuth connect, Media voices, PXPipe detail,
// Translator send, CLI settings, Pricing, Core system)
// -------------------------------------------------------------

// OAuthAuthorize starts an OAuth flow: GET /api/oauth/{provider}/authorize.
func (c *HTTPCoreClient) OAuthAuthorize(ctx context.Context, provider, redirectURI string) (map[string]interface{}, error) {
	ep := fmt.Sprintf("/api/oauth/%s/authorize?redirect_uri=%s", provider, url.QueryEscape(redirectURI))
	data, err := c.doRequest(ctx, http.MethodGet, ep, nil)
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// OAuthExchange completes an OAuth flow: POST /api/oauth/{provider}/exchange.
func (c *HTTPCoreClient) OAuthExchange(ctx context.Context, provider string, payload map[string]interface{}) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodPost, fmt.Sprintf("/api/oauth/%s/exchange", provider), payload)
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// KiroSocialAuthorize starts Kiro Google/GitHub OAuth: provider=google|github.
func (c *HTTPCoreClient) KiroSocialAuthorize(ctx context.Context, idp, redirectURI string) (map[string]interface{}, error) {
	ep := fmt.Sprintf("/api/oauth/kiro/social-authorize?provider=%s&redirect_uri=%s", idp, url.QueryEscape(redirectURI))
	data, err := c.doRequest(ctx, http.MethodGet, ep, nil)
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// KiroSocialExchange completes Kiro social OAuth.
func (c *HTTPCoreClient) KiroSocialExchange(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodPost, "/api/oauth/kiro/social-exchange", payload)
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// MediaVoices lists TTS voices for an engine (deepgram/inworld/elevenlabs/minimax)
// or the generic catalog when engine is "".
func (c *HTTPCoreClient) MediaVoices(ctx context.Context, engine string) (map[string]interface{}, error) {
	ep := "/api/media-providers/tts/voices"
	if engine != "" {
		ep = fmt.Sprintf("/api/media-providers/tts/%s/voices", engine)
	}
	data, err := c.doRequest(ctx, http.MethodGet, ep, nil)
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	if err := json.Unmarshal(data, &res); err != nil {
		// Core double-encodes this payload as a JSON string; decode once more.
		var inner string
		if err2 := json.Unmarshal(data, &inner); err2 == nil {
			_ = json.Unmarshal([]byte(inner), &res)
		}
	}
	return res, nil
}

// PxpipeLogs proxies GET /api/pxpipe/logs from 9router Core.
func (c *HTTPCoreClient) PxpipeLogs(ctx context.Context) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodGet, "/api/pxpipe/logs", nil)
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// PxpipeHealth proxies GET /api/pxpipe/health from 9router Core.
func (c *HTTPCoreClient) PxpipeHealth(ctx context.Context) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodGet, "/api/pxpipe/health", nil)
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// TranslatorSend proxies POST /api/translator/send (needs provider+model+body).
func (c *HTTPCoreClient) TranslatorSend(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodPost, "/api/translator/send", payload)
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// CLIToolSettings proxies GET /api/cli-tools/{tool}-settings (claude, codex,
// opencode, openclaw, grok-build, kilo, devin, droid, deepseek-tui, jcode,
// cline, copilot, cowork, hermes).
func (c *HTTPCoreClient) CLIToolSettings(ctx context.Context, tool string) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodGet, fmt.Sprintf("/api/cli-tools/%s-settings", tool), nil)
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// Pricing proxies GET /api/pricing (per-model $/1M tokens: gh + tokenrouter).
func (c *HTTPCoreClient) Pricing(ctx context.Context) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodGet, "/api/pricing", nil)
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// CoreVersion proxies GET /api/version (currentVersion/latestVersion/hasUpdate).
func (c *HTTPCoreClient) CoreVersion(ctx context.Context) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodGet, "/api/version", nil)
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// CoreKeys proxies GET /api/keys (machine API keys registered in core).
func (c *HTTPCoreClient) CoreKeys(ctx context.Context) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodGet, "/api/keys", nil)
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

// mergedModelPrefixes are LLM providers known to be filtered out of core's
// /v1/models exposure list. Used as fallback when the customModels table
// cannot be read; the primary source is always the core DB customModels scope.
var mergedModelPrefixes = []string{"oc/", "opencode-go/"}

// customModelIDs reads core kv scope 'customModels' (best-effort) and returns
// the set of expected OpenAI IDs ("alias/id"). Empty set on any error so the
// caller falls back to mergedModelPrefixes.
func (c *HTTPCoreClient) customModelIDs(ctx context.Context) map[string]bool {
	out := map[string]bool{}
	dsn := fmt.Sprintf("%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", c.cfg.GetNineRouterDBPath())
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return out
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, "SELECT value FROM kv WHERE scope = 'customModels'")
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			continue
		}
		var m struct {
			ProviderAlias string `json:"providerAlias"`
			ID            string `json:"id"`
		}
		if err := json.Unmarshal([]byte(v), &m); err != nil {
			continue
		}
		if m.ProviderAlias != "" && m.ID != "" {
			out[m.ProviderAlias+"/"+m.ID] = true
		}
	}
	return out
}

// GetMergedModels returns core's /v1/models list merged with catalog models
// that core filters out of OpenAI exposure (notably oc/* + opencode-go/*
// OpenCode Zen free tier). Primary merge source is the core DB customModels
// scope (generic guard for future prefixes); mergedModelPrefixes is fallback.
// Results are cached in-memory with stale-while-revalidate to prevent blocking callers.
func (c *HTTPCoreClient) GetMergedModels(ctx context.Context) (string, []map[string]interface{}, error) {
	c.modelsMu.RLock()
	if c.cachedModels != nil {
		age := time.Since(c.cachedModelsAt)
		if age < 3*time.Minute {
			obj, data := c.cachedObj, c.cachedModels
			c.modelsMu.RUnlock()
			return obj, data, nil
		}
		if age < 15*time.Minute {
			obj, data := c.cachedObj, c.cachedModels
			shouldFetch := !c.modelsFetching
			c.modelsMu.RUnlock()
			if shouldFetch {
				go func() {
					c.modelsMu.Lock()
					if c.modelsFetching {
						c.modelsMu.Unlock()
						return
					}
					c.modelsFetching = true
					c.modelsMu.Unlock()

					defer func() {
						c.modelsMu.Lock()
						c.modelsFetching = false
						c.modelsMu.Unlock()
					}()

					bgCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					_, _, _ = c.fetchAndCacheMergedModels(bgCtx)
				}()
			}
			return obj, data, nil
		}
	}
	c.modelsMu.RUnlock()

	return c.fetchAndCacheMergedModels(ctx)
}

func (c *HTTPCoreClient) fetchAndCacheMergedModels(ctx context.Context) (string, []map[string]interface{}, error) {
	rawV1, err := c.doRequest(ctx, http.MethodGet, "/v1/models", nil)
	if err != nil {
		c.modelsMu.RLock()
		if c.cachedModels != nil {
			obj, data := c.cachedObj, c.cachedModels
			c.modelsMu.RUnlock()
			return obj, data, nil
		}
		c.modelsMu.RUnlock()
		return "", nil, err
	}
	var v1 struct {
		Object string                   `json:"object"`
		Data   []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(rawV1, &v1); err != nil {
		c.modelsMu.RLock()
		if c.cachedModels != nil {
			obj, data := c.cachedObj, c.cachedModels
			c.modelsMu.RUnlock()
			return obj, data, nil
		}
		c.modelsMu.RUnlock()
		return "", nil, err
	}
	if v1.Object == "" {
		v1.Object = "list"
	}
	existing := make(map[string]bool, len(v1.Data))
	for _, m := range v1.Data {
		if id, ok := m["id"].(string); ok {
			existing[id] = true
		}
	}

	rawCat, err := c.doRequest(ctx, http.MethodGet, "/api/models", nil)
	if err != nil {
		c.cacheMergedResult(v1.Object, v1.Data)
		return v1.Object, v1.Data, nil
	}
	var cat struct {
		Models []struct {
			Provider    string                 `json:"provider"`
			Model       string                 `json:"model"`
			FullModel   string                 `json:"fullModel"`
			RoutedModel string                 `json:"routedModel"`
			Name        string                 `json:"name"`
			Caps        map[string]interface{} `json:"caps"`
		} `json:"models"`
	}
	if err := json.Unmarshal(rawCat, &cat); err != nil || len(cat.Models) == 0 {
		c.cacheMergedResult(v1.Object, v1.Data)
		return v1.Object, v1.Data, nil
	}

	want := c.customModelIDs(ctx)
	useWant := len(want) > 0
	for _, m := range cat.Models {
		id := m.FullModel
		if id == "" {
			id = m.Provider + "/" + m.Model
		}
		if existing[id] {
			continue
		}
		merge := false
		if useWant {
			// oc/* free tier is proven reachable without a linked OAuth
			// account, but core does not register every oc model in
			// customModels (e.g. oc/muse-spark-*-contributor-free) — so
			// always merge the oc/ prefix. opencode-go/* stays
			// customModels-gated: core rejects it with "No active
			// credentials" when no account is linked.
			merge = want[id] || strings.HasPrefix(id, "oc/")
		} else {
			for _, p := range mergedModelPrefixes {
				if strings.HasPrefix(id, p) {
					merge = true
					break
				}
			}
		}
		if !merge {
			continue
		}
		entry := map[string]interface{}{
			"id":       id,
			"object":   "model",
			"owned_by": m.Provider,
		}
		if m.Name != "" {
			entry["display_name"] = m.Name
		}
		if len(m.Caps) > 0 {
			entry["capabilities"] = m.Caps
			if cw, ok := m.Caps["contextWindow"]; ok {
				entry["context_length"] = cw
			}
			if mo, ok := m.Caps["maxOutput"]; ok {
				entry["max_completion_tokens"] = mo
			}
		}
		v1.Data = append(v1.Data, entry)
		existing[id] = true
	}
	c.cacheMergedResult(v1.Object, v1.Data)
	return v1.Object, v1.Data, nil
}

func (c *HTTPCoreClient) cacheMergedResult(obj string, data []map[string]interface{}) {
	if len(data) == 0 {
		return
	}
	c.modelsMu.Lock()
	c.cachedObj = obj
	c.cachedModels = data
	c.cachedModelsAt = time.Now()
	c.modelsMu.Unlock()
}
