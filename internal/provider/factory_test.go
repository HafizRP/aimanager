package provider

import (
	"testing"
)

func TestProviderClientFactory_BuiltinProviders(t *testing.T) {
	factory := NewProviderClientFactory()

	// 1. Antigravity
	agClient, err := factory.Create(ProviderConfig{
		Type:    ProviderAntigravity,
		BaseURL: "http://127.0.0.1:20128",
	})
	if err != nil {
		t.Fatalf("expected Antigravity client creation success, got %v", err)
	}
	if agClient.Type() != ProviderAntigravity {
		t.Errorf("expected ProviderAntigravity, got %s", agClient.Type())
	}
	agHeaders := agClient.PrepareHeaders("test-token")
	if agHeaders["Authorization"] != "Bearer test-token" {
		t.Errorf("expected Bearer token header, got %s", agHeaders["Authorization"])
	}
	if model := agClient.TransformModel("ag/gemini-3.8-flash"); model != "gemini-3.8-flash" {
		t.Errorf("expected stripped model, got %s", model)
	}

	// 2. Kiro
	krClient, err := factory.Create(ProviderConfig{
		Type:    ProviderKiro,
		BaseURL: "http://127.0.0.1:20128",
	})
	if err != nil {
		t.Fatalf("expected Kiro client creation success, got %v", err)
	}
	if model := krClient.TransformModel("kr/claude-3-5-sonnet"); model != "claude-3-5-sonnet" {
		t.Errorf("expected stripped model, got %s", model)
	}

	// 3. Anthropic
	anthClient, err := factory.Create(ProviderConfig{
		Type:   ProviderAnthropic,
		APIKey: "sk-ant-123",
	})
	if err != nil {
		t.Fatalf("expected Anthropic client creation success, got %v", err)
	}
	anthHeaders := anthClient.PrepareHeaders("")
	if anthHeaders["x-api-key"] != "sk-ant-123" {
		t.Errorf("expected x-api-key sk-ant-123, got %s", anthHeaders["x-api-key"])
	}

	// 4. OpenRouter
	orClient, err := factory.Create(ProviderConfig{
		Type:   ProviderOpenRouter,
		APIKey: "sk-or-456",
	})
	if err != nil {
		t.Fatalf("expected OpenRouter client creation success, got %v", err)
	}
	orHeaders := orClient.PrepareHeaders("")
	if orHeaders["HTTP-Referer"] != "https://aimanager.b14.my.id" {
		t.Errorf("expected HTTP-Referer header, got %s", orHeaders["HTTP-Referer"])
	}

	// 5. OpenAI Compatible
	oaiClient, err := factory.Create(ProviderConfig{
		Type:    ProviderOpenAICompatible,
		BaseURL: "https://api.groq.com/openai/v1",
		APIKey:  "gsk-789",
		Prefix:  "groq/",
	})
	if err != nil {
		t.Fatalf("expected OpenAI-compatible client creation success, got %v", err)
	}
	if model := oaiClient.TransformModel("groq/llama-3.3-70b"); model != "llama-3.3-70b" {
		t.Errorf("expected stripped prefix model, got %s", model)
	}
}

func TestProviderClientFactory_CustomProviderRegistration(t *testing.T) {
	factory := NewProviderClientFactory()

	customType := ProviderType("ollama")
	factory.Register(customType, func(cfg ProviderConfig) (ProviderClient, error) {
		if cfg.BaseURL == "" {
			cfg.BaseURL = "http://localhost:11434"
		}
		return NewOpenAICompatibleClient(cfg), nil
	})

	client, err := factory.Create(ProviderConfig{
		Type:    customType,
		BaseURL: "http://localhost:11434",
	})
	if err != nil {
		t.Fatalf("expected custom provider creation to succeed, got %v", err)
	}
	if client.BaseURL() != "http://localhost:11434" {
		t.Errorf("expected localhost:11434, got %s", client.BaseURL())
	}
}

func TestProviderClientFactory_UnsupportedType(t *testing.T) {
	factory := NewProviderClientFactory()

	_, err := factory.Create(ProviderConfig{
		Type: ProviderType("nonexistent-vendor"),
	})
	if err == nil {
		t.Fatal("expected error for unsupported provider type, got nil")
	}
}
