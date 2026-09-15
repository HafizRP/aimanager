// Package provider implements the Factory pattern for initializing and configuring upstream AI providers.
package provider

import (
	"fmt"
	"strings"
)

// BaseProvider holds common provider connection attributes.
type BaseProvider struct {
	cfg ProviderConfig
}

func (b *BaseProvider) Name() string {
	if b.cfg.Name != "" {
		return b.cfg.Name
	}
	return string(b.cfg.Type)
}

func (b *BaseProvider) BaseURL() string {
	return b.cfg.BaseURL
}

func (b *BaseProvider) ValidateConfig() error {
	if b.cfg.BaseURL == "" {
		return fmt.Errorf("base_url is required")
	}
	return nil
}

// 1. Antigravity Provider
type AntigravityClient struct {
	BaseProvider
}

func NewAntigravityClient(cfg ProviderConfig) *AntigravityClient {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://127.0.0.1:20128"
	}
	if cfg.Prefix == "" {
		cfg.Prefix = "ag/"
	}
	return &AntigravityClient{BaseProvider{cfg: cfg}}
}

func (c *AntigravityClient) Type() ProviderType {
	return ProviderAntigravity
}

func (c *AntigravityClient) PrepareHeaders(clientAuthKey string) map[string]string {
	headers := map[string]string{
		"Authorization": "Bearer " + clientAuthKey,
		"X-Provider":    "antigravity",
	}
	for k, v := range c.cfg.Headers {
		headers[k] = v
	}
	return headers
}

func (c *AntigravityClient) TransformModel(model string) string {
	return strings.TrimPrefix(model, "ag/")
}

// 2. Kiro AI Provider
type KiroClient struct {
	BaseProvider
}

func NewKiroClient(cfg ProviderConfig) *KiroClient {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://127.0.0.1:20128"
	}
	if cfg.Prefix == "" {
		cfg.Prefix = "kr/"
	}
	return &KiroClient{BaseProvider{cfg: cfg}}
}

func (c *KiroClient) Type() ProviderType {
	return ProviderKiro
}

func (c *KiroClient) PrepareHeaders(clientAuthKey string) map[string]string {
	headers := map[string]string{
		"Authorization": "Bearer " + clientAuthKey,
		"X-Provider":    "kiro",
	}
	for k, v := range c.cfg.Headers {
		headers[k] = v
	}
	return headers
}

func (c *KiroClient) TransformModel(model string) string {
	return strings.TrimPrefix(model, "kr/")
}

// 3. OpenCode Zen Provider
type OpenCodeClient struct {
	BaseProvider
}

func NewOpenCodeClient(cfg ProviderConfig) *OpenCodeClient {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://opencode.ai/zen/v1/responses"
	}
	if cfg.Prefix == "" {
		cfg.Prefix = "oc/"
	}
	return &OpenCodeClient{BaseProvider{cfg: cfg}}
}

func (c *OpenCodeClient) Type() ProviderType {
	return ProviderOpenCode
}

func (c *OpenCodeClient) PrepareHeaders(clientAuthKey string) map[string]string {
	headers := map[string]string{
		"Authorization":      "Bearer " + clientAuthKey,
		"X-OpenCode-Gateway": "aimanager",
	}
	for k, v := range c.cfg.Headers {
		headers[k] = v
	}
	return headers
}

func (c *OpenCodeClient) TransformModel(model string) string {
	if strings.HasPrefix(model, "oc/") {
		return strings.TrimPrefix(model, "oc/")
	}
	return strings.TrimPrefix(model, "opencode-go/")
}

// 4. OpenAI-Compatible Provider
type OpenAICompatibleClient struct {
	BaseProvider
}

func NewOpenAICompatibleClient(cfg ProviderConfig) *OpenAICompatibleClient {
	return &OpenAICompatibleClient{BaseProvider{cfg: cfg}}
}

func (c *OpenAICompatibleClient) Type() ProviderType {
	return ProviderOpenAICompatible
}

func (c *OpenAICompatibleClient) PrepareHeaders(clientAuthKey string) map[string]string {
	key := c.cfg.APIKey
	if key == "" {
		key = clientAuthKey
	}
	headers := map[string]string{
		"Authorization": "Bearer " + key,
	}
	for k, v := range c.cfg.Headers {
		headers[k] = v
	}
	return headers
}

func (c *OpenAICompatibleClient) TransformModel(model string) string {
	if c.cfg.Prefix != "" && strings.HasPrefix(model, c.cfg.Prefix) {
		return strings.TrimPrefix(model, c.cfg.Prefix)
	}
	return model
}

// 5. Anthropic Provider
type AnthropicClient struct {
	BaseProvider
}

func NewAnthropicClient(cfg ProviderConfig) *AnthropicClient {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.anthropic.com/v1"
	}
	return &AnthropicClient{BaseProvider{cfg: cfg}}
}

func (c *AnthropicClient) Type() ProviderType {
	return ProviderAnthropic
}

func (c *AnthropicClient) PrepareHeaders(clientAuthKey string) map[string]string {
	key := c.cfg.APIKey
	if key == "" {
		key = clientAuthKey
	}
	headers := map[string]string{
		"x-api-key":         key,
		"anthropic-version": "2023-06-01",
	}
	for k, v := range c.cfg.Headers {
		headers[k] = v
	}
	return headers
}

func (c *AnthropicClient) TransformModel(model string) string {
	return strings.TrimPrefix(model, "anthropic/")
}

// 6. OpenRouter Provider
type OpenRouterClient struct {
	BaseProvider
}

func NewOpenRouterClient(cfg ProviderConfig) *OpenRouterClient {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://openrouter.ai/api/v1"
	}
	return &OpenRouterClient{BaseProvider{cfg: cfg}}
}

func (c *OpenRouterClient) Type() ProviderType {
	return ProviderOpenRouter
}

func (c *OpenRouterClient) PrepareHeaders(clientAuthKey string) map[string]string {
	key := c.cfg.APIKey
	if key == "" {
		key = clientAuthKey
	}
	headers := map[string]string{
		"Authorization": "Bearer " + key,
		"HTTP-Referer":  "https://aimanager.b14.my.id",
		"X-Title":       "AI Manager Gateway",
	}
	for k, v := range c.cfg.Headers {
		headers[k] = v
	}
	return headers
}

func (c *OpenRouterClient) TransformModel(model string) string {
	return strings.TrimPrefix(model, "openrouter/")
}
