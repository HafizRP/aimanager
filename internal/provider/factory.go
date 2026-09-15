// Package provider implements the Factory pattern for initializing and configuring upstream AI providers.
package provider

import (
	"fmt"
	"strings"
	"sync"
)

// ProviderType identifies the upstream provider flavor.
type ProviderType string

const (
	ProviderAntigravity      ProviderType = "antigravity"
	ProviderKiro             ProviderType = "kiro"
	ProviderOpenCode         ProviderType = "opencode"
	ProviderOpenAICompatible ProviderType = "openai-compatible"
	ProviderAnthropic        ProviderType = "anthropic"
	ProviderOpenRouter       ProviderType = "openrouter"
	ProviderCustom           ProviderType = "custom"
)

// ProviderConfig specifies the parameters needed to initialize a provider.
type ProviderConfig struct {
	Type        ProviderType      `json:"type"`
	Name        string            `json:"name"`
	BaseURL     string            `json:"base_url"`
	APIKey      string            `json:"api_key"`
	Prefix      string            `json:"prefix"`
	Headers     map[string]string `json:"headers,omitempty"`
	ExtraConfig map[string]any    `json:"extra_config,omitempty"`
}

// ProviderClient encapsulates provider-specific authentication, headers, and model translation.
type ProviderClient interface {
	Type() ProviderType
	Name() string
	BaseURL() string
	PrepareHeaders(clientAuthKey string) map[string]string
	TransformModel(model string) string
	ValidateConfig() error
}

// ProviderCreator is a factory function that instantiates a ProviderClient from a configuration.
type ProviderCreator func(cfg ProviderConfig) (ProviderClient, error)

// ProviderClientFactory manages registered provider creators.
type ProviderClientFactory struct {
	mu       sync.RWMutex
	creators map[ProviderType]ProviderCreator
}

// NewProviderClientFactory initializes a factory pre-loaded with built-in provider implementations.
func NewProviderClientFactory() *ProviderClientFactory {
	f := &ProviderClientFactory{
		creators: make(map[ProviderType]ProviderCreator),
	}

	// Register built-in providers
	f.Register(ProviderAntigravity, func(cfg ProviderConfig) (ProviderClient, error) {
		return NewAntigravityClient(cfg), nil
	})
	f.Register(ProviderKiro, func(cfg ProviderConfig) (ProviderClient, error) {
		return NewKiroClient(cfg), nil
	})
	f.Register(ProviderOpenCode, func(cfg ProviderConfig) (ProviderClient, error) {
		return NewOpenCodeClient(cfg), nil
	})
	f.Register(ProviderOpenAICompatible, func(cfg ProviderConfig) (ProviderClient, error) {
		return NewOpenAICompatibleClient(cfg), nil
	})
	f.Register(ProviderAnthropic, func(cfg ProviderConfig) (ProviderClient, error) {
		return NewAnthropicClient(cfg), nil
	})
	f.Register(ProviderOpenRouter, func(cfg ProviderConfig) (ProviderClient, error) {
		return NewOpenRouterClient(cfg), nil
	})

	return f
}

// Register registers a custom provider creator for a given provider type.
func (f *ProviderClientFactory) Register(pType ProviderType, creator ProviderCreator) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.creators[pType] = creator
}

// Create instantiates the appropriate ProviderClient based on the configuration's Type.
func (f *ProviderClientFactory) Create(cfg ProviderConfig) (ProviderClient, error) {
	f.mu.RLock()
	creator, exists := f.creators[cfg.Type]
	f.mu.RUnlock()

	if !exists {
		// Fallback: if unknown or prefixed with openai-compatible, use OpenAI-compatible
		if strings.HasPrefix(string(cfg.Type), "openai-compatible") {
			return NewOpenAICompatibleClient(cfg), nil
		}
		return nil, fmt.Errorf("unsupported provider type: %s", cfg.Type)
	}

	client, err := creator(cfg)
	if err != nil {
		return nil, err
	}

	if err := client.ValidateConfig(); err != nil {
		return nil, fmt.Errorf("provider config validation failed: %w", err)
	}

	return client, nil
}
