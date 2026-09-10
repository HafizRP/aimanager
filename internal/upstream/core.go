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

type CoreClient struct {
	cfg        *config.Config
	httpClient *http.Client
	mu         sync.RWMutex
	cliToken   string
}

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

func (c *CoreClient) ToggleProvider(ctx context.Context, id string, isActive bool) error {
	payload := map[string]interface{}{
		"isActive": isActive,
	}
	_, err := c.doRequest(ctx, http.MethodPut, fmt.Sprintf("/api/providers/%s", id), payload)
	return err
}

func (c *CoreClient) SetProviderPriority(ctx context.Context, id string, priority int) error {
	payload := map[string]interface{}{
		"priority": priority,
	}
	_, err := c.doRequest(ctx, http.MethodPut, fmt.Sprintf("/api/providers/%s", id), payload)
	return err
}

func (c *CoreClient) TestProvider(ctx context.Context, id string) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodPost, fmt.Sprintf("/api/providers/%s/test", id), nil)
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}

func (c *CoreClient) DeleteProvider(ctx context.Context, id string) error {
	_, err := c.doRequest(ctx, http.MethodDelete, fmt.Sprintf("/api/providers/%s", id), nil)
	return err
}

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

type Combo struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Kind      *string  `json:"kind"`
	Models    []string `json:"models"`
	CreatedAt string   `json:"createdAt"`
	UpdatedAt string   `json:"updatedAt"`
}

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

func (c *CoreClient) CreateCombo(ctx context.Context, name string, models []string) error {
	payload := map[string]interface{}{
		"name":   name,
		"models": models,
	}
	_, err := c.doRequest(ctx, http.MethodPost, "/api/combos", payload)
	return err
}

func (c *CoreClient) UpdateCombo(ctx context.Context, id string, name string, models []string) error {
	payload := map[string]interface{}{
		"name":   name,
		"models": models,
	}
	_, err := c.doRequest(ctx, http.MethodPut, fmt.Sprintf("/api/combos/%s", id), payload)
	return err
}

func (c *CoreClient) DeleteCombo(ctx context.Context, id string) error {
	_, err := c.doRequest(ctx, http.MethodDelete, fmt.Sprintf("/api/combos/%s", id), nil)
	return err
}

// -------------------------------------------------------------
// Settings & Token Saver
// -------------------------------------------------------------

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

func (c *CoreClient) UpdateSettings(ctx context.Context, updates map[string]interface{}) error {
	_, err := c.doRequest(ctx, http.MethodPost, "/api/settings", updates)
	return err
}

// -------------------------------------------------------------
// Model Aliases
// -------------------------------------------------------------

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

func (c *CoreClient) SetModelAlias(ctx context.Context, alias, model string) error {
	payload := map[string]string{
		"alias": alias,
		"model": model,
	}
	_, err := c.doRequest(ctx, http.MethodPut, "/api/models/alias", payload)
	return err
}

func (c *CoreClient) DeleteModelAlias(ctx context.Context, alias string) error {
	ep := fmt.Sprintf("/api/models/alias?alias=%s", strings.TrimSpace(alias))
	_, err := c.doRequest(ctx, http.MethodDelete, ep, nil)
	return err
}

// -------------------------------------------------------------
// Proxy Pools
// -------------------------------------------------------------

type ProxyPool struct {
	ID         string                 `json:"id"`
	IsActive   bool                   `json:"isActive"`
	TestStatus string                 `json:"testStatus"`
	Data       map[string]interface{} `json:"data"`
	CreatedAt  string                 `json:"createdAt"`
	UpdatedAt  string                 `json:"updatedAt"`
}

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

func (c *CoreClient) CreateProxyPool(ctx context.Context, payload map[string]interface{}) error {
	_, err := c.doRequest(ctx, http.MethodPost, "/api/proxy-pools", payload)
	return err
}

func (c *CoreClient) DeleteProxyPool(ctx context.Context, id string) error {
	_, err := c.doRequest(ctx, http.MethodDelete, fmt.Sprintf("/api/proxy-pools/%s", id), nil)
	return err
}

func (c *CoreClient) TestProxyPool(ctx context.Context, id string) (map[string]interface{}, error) {
	data, err := c.doRequest(ctx, http.MethodPost, fmt.Sprintf("/api/proxy-pools/%s/test", id), nil)
	if err != nil {
		return nil, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	return res, nil
}
