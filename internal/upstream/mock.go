package upstream

import (
	"context"
)

// MockCoreClient provides an in-memory mock implementation of the CoreClient interface for unit tests.
type MockCoreClient struct {
	ResetCLITokenFunc        func()
	GetProvidersFunc         func(ctx context.Context) ([]ProviderConnection, error)
	ToggleProviderFunc       func(ctx context.Context, id string, isActive bool) error
	SetProviderPriorityFunc  func(ctx context.Context, id string, priority int) error
	TestProviderFunc         func(ctx context.Context, id string) (map[string]interface{}, error)
	DeleteProviderFunc       func(ctx context.Context, id string) error
	CreateProviderFunc       func(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error)
	GetCombosFunc            func(ctx context.Context) ([]Combo, error)
	CreateComboFunc          func(ctx context.Context, name string, models []string) error
	UpdateComboFunc          func(ctx context.Context, id string, name string, models []string) error
	DeleteComboFunc          func(ctx context.Context, id string) error
	GetSettingsFunc          func(ctx context.Context) (map[string]interface{}, error)
	UpdateSettingsFunc       func(ctx context.Context, updates map[string]interface{}) error
	GetModelAliasesFunc      func(ctx context.Context) (map[string]string, error)
	SetModelAliasFunc        func(ctx context.Context, alias, model string) error
	DeleteModelAliasFunc     func(ctx context.Context, alias string) error
	GetProxyPoolsFunc        func(ctx context.Context) ([]ProxyPool, error)
	CreateProxyPoolFunc      func(ctx context.Context, payload map[string]interface{}) error
	DeleteProxyPoolFunc      func(ctx context.Context, id string) error
	TestProxyPoolFunc        func(ctx context.Context, id string) (map[string]interface{}, error)
	ServiceStatusFunc        func(ctx context.Context, service string) (map[string]interface{}, error)
	ServiceStatsFunc         func(ctx context.Context, service string) (map[string]interface{}, error)
	ServiceActionFunc        func(ctx context.Context, service, action string, payload map[string]interface{}) (map[string]interface{}, error)
	TranslatorTranslateFunc  func(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error)
	UsageStatsFunc           func(ctx context.Context) (map[string]interface{}, error)
	ConsoleLogsFunc          func(ctx context.Context) (map[string]interface{}, error)
	GetProviderNodesFunc     func(ctx context.Context) ([]ProviderNode, error)
	CreateProviderNodeFunc   func(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error)
	UpdateProviderNodeFunc   func(ctx context.Context, id string, payload map[string]interface{}) (map[string]interface{}, error)
	DeleteProviderNodeFunc   func(ctx context.Context, id string) error
	ValidateProviderNodeFunc func(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error)
	MitmStatusFunc           func(ctx context.Context) (map[string]interface{}, error)
	MitmStartFunc            func(ctx context.Context, apiKey string) (map[string]interface{}, error)
	MitmStopFunc             func(ctx context.Context) (map[string]interface{}, error)
	MitmDNSFunc              func(ctx context.Context, tool, action string) (map[string]interface{}, error)
	MitmAliasGetFunc         func(ctx context.Context, tool string) (map[string]interface{}, error)
	MitmAliasPutFunc         func(ctx context.Context, tool string, mappings map[string]interface{}) (map[string]interface{}, error)
	MCPRegistryFunc          func(ctx context.Context) (map[string]interface{}, error)
	MCPInspectFunc           func(ctx context.Context, url string) (map[string]interface{}, error)
	OAuthAuthorizeFunc       func(ctx context.Context, provider, redirectURI string) (map[string]interface{}, error)
	OAuthExchangeFunc        func(ctx context.Context, provider string, payload map[string]interface{}) (map[string]interface{}, error)
	KiroSocialAuthorizeFunc  func(ctx context.Context, idp, redirectURI string) (map[string]interface{}, error)
	KiroSocialExchangeFunc   func(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error)
	MediaVoicesFunc          func(ctx context.Context, engine string) (map[string]interface{}, error)
	PxpipeLogsFunc           func(ctx context.Context) (map[string]interface{}, error)
	PxpipeHealthFunc         func(ctx context.Context) (map[string]interface{}, error)
	TranslatorSendFunc       func(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error)
	CLIToolSettingsFunc      func(ctx context.Context, tool string) (map[string]interface{}, error)
	PricingFunc              func(ctx context.Context) (map[string]interface{}, error)
	CoreVersionFunc          func(ctx context.Context) (map[string]interface{}, error)
	CoreKeysFunc             func(ctx context.Context) (map[string]interface{}, error)
	GetMergedModelsFunc      func(ctx context.Context) (string, []map[string]interface{}, error)
}

var _ CoreClient = (*MockCoreClient)(nil)

func (m *MockCoreClient) ResetCLIToken() {
	if m.ResetCLITokenFunc != nil {
		m.ResetCLITokenFunc()
	}
}

func (m *MockCoreClient) GetProviders(ctx context.Context) ([]ProviderConnection, error) {
	if m.GetProvidersFunc != nil {
		return m.GetProvidersFunc(ctx)
	}
	return nil, nil
}

func (m *MockCoreClient) ToggleProvider(ctx context.Context, id string, isActive bool) error {
	if m.ToggleProviderFunc != nil {
		return m.ToggleProviderFunc(ctx, id, isActive)
	}
	return nil
}

func (m *MockCoreClient) SetProviderPriority(ctx context.Context, id string, priority int) error {
	if m.SetProviderPriorityFunc != nil {
		return m.SetProviderPriorityFunc(ctx, id, priority)
	}
	return nil
}

func (m *MockCoreClient) TestProvider(ctx context.Context, id string) (map[string]interface{}, error) {
	if m.TestProviderFunc != nil {
		return m.TestProviderFunc(ctx, id)
	}
	return map[string]interface{}{"valid": true}, nil
}

func (m *MockCoreClient) DeleteProvider(ctx context.Context, id string) error {
	if m.DeleteProviderFunc != nil {
		return m.DeleteProviderFunc(ctx, id)
	}
	return nil
}

func (m *MockCoreClient) CreateProvider(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	if m.CreateProviderFunc != nil {
		return m.CreateProviderFunc(ctx, payload)
	}
	return map[string]interface{}{"success": true}, nil
}

func (m *MockCoreClient) GetCombos(ctx context.Context) ([]Combo, error) {
	if m.GetCombosFunc != nil {
		return m.GetCombosFunc(ctx)
	}
	return nil, nil
}

func (m *MockCoreClient) CreateCombo(ctx context.Context, name string, models []string) error {
	if m.CreateComboFunc != nil {
		return m.CreateComboFunc(ctx, name, models)
	}
	return nil
}

func (m *MockCoreClient) UpdateCombo(ctx context.Context, id string, name string, models []string) error {
	if m.UpdateComboFunc != nil {
		return m.UpdateComboFunc(ctx, id, name, models)
	}
	return nil
}

func (m *MockCoreClient) DeleteCombo(ctx context.Context, id string) error {
	if m.DeleteComboFunc != nil {
		return m.DeleteComboFunc(ctx, id)
	}
	return nil
}

func (m *MockCoreClient) GetSettings(ctx context.Context) (map[string]interface{}, error) {
	if m.GetSettingsFunc != nil {
		return m.GetSettingsFunc(ctx)
	}
	return map[string]interface{}{}, nil
}

func (m *MockCoreClient) UpdateSettings(ctx context.Context, updates map[string]interface{}) error {
	if m.UpdateSettingsFunc != nil {
		return m.UpdateSettingsFunc(ctx, updates)
	}
	return nil
}

func (m *MockCoreClient) GetModelAliases(ctx context.Context) (map[string]string, error) {
	if m.GetModelAliasesFunc != nil {
		return m.GetModelAliasesFunc(ctx)
	}
	return map[string]string{}, nil
}

func (m *MockCoreClient) SetModelAlias(ctx context.Context, alias, model string) error {
	if m.SetModelAliasFunc != nil {
		return m.SetModelAliasFunc(ctx, alias, model)
	}
	return nil
}

func (m *MockCoreClient) DeleteModelAlias(ctx context.Context, alias string) error {
	if m.DeleteModelAliasFunc != nil {
		return m.DeleteModelAliasFunc(ctx, alias)
	}
	return nil
}

func (m *MockCoreClient) GetProxyPools(ctx context.Context) ([]ProxyPool, error) {
	if m.GetProxyPoolsFunc != nil {
		return m.GetProxyPoolsFunc(ctx)
	}
	return nil, nil
}

func (m *MockCoreClient) CreateProxyPool(ctx context.Context, payload map[string]interface{}) error {
	if m.CreateProxyPoolFunc != nil {
		return m.CreateProxyPoolFunc(ctx, payload)
	}
	return nil
}

func (m *MockCoreClient) DeleteProxyPool(ctx context.Context, id string) error {
	if m.DeleteProxyPoolFunc != nil {
		return m.DeleteProxyPoolFunc(ctx, id)
	}
	return nil
}

func (m *MockCoreClient) TestProxyPool(ctx context.Context, id string) (map[string]interface{}, error) {
	if m.TestProxyPoolFunc != nil {
		return m.TestProxyPoolFunc(ctx, id)
	}
	return map[string]interface{}{"valid": true}, nil
}

func (m *MockCoreClient) ServiceStatus(ctx context.Context, service string) (map[string]interface{}, error) {
	if m.ServiceStatusFunc != nil {
		return m.ServiceStatusFunc(ctx, service)
	}
	return map[string]interface{}{"status": "running"}, nil
}

func (m *MockCoreClient) ServiceStats(ctx context.Context, service string) (map[string]interface{}, error) {
	if m.ServiceStatsFunc != nil {
		return m.ServiceStatsFunc(ctx, service)
	}
	return map[string]interface{}{}, nil
}

func (m *MockCoreClient) ServiceAction(ctx context.Context, service, action string, payload map[string]interface{}) (map[string]interface{}, error) {
	if m.ServiceActionFunc != nil {
		return m.ServiceActionFunc(ctx, service, action, payload)
	}
	return map[string]interface{}{"success": true}, nil
}

func (m *MockCoreClient) TranslatorTranslate(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	if m.TranslatorTranslateFunc != nil {
		return m.TranslatorTranslateFunc(ctx, payload)
	}
	return map[string]interface{}{}, nil
}

func (m *MockCoreClient) UsageStats(ctx context.Context) (map[string]interface{}, error) {
	if m.UsageStatsFunc != nil {
		return m.UsageStatsFunc(ctx)
	}
	return map[string]interface{}{}, nil
}

func (m *MockCoreClient) ConsoleLogs(ctx context.Context) (map[string]interface{}, error) {
	if m.ConsoleLogsFunc != nil {
		return m.ConsoleLogsFunc(ctx)
	}
	return map[string]interface{}{"logs": []string{}}, nil
}

func (m *MockCoreClient) GetProviderNodes(ctx context.Context) ([]ProviderNode, error) {
	if m.GetProviderNodesFunc != nil {
		return m.GetProviderNodesFunc(ctx)
	}
	return nil, nil
}

func (m *MockCoreClient) CreateProviderNode(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	if m.CreateProviderNodeFunc != nil {
		return m.CreateProviderNodeFunc(ctx, payload)
	}
	return map[string]interface{}{"success": true}, nil
}

func (m *MockCoreClient) UpdateProviderNode(ctx context.Context, id string, payload map[string]interface{}) (map[string]interface{}, error) {
	if m.UpdateProviderNodeFunc != nil {
		return m.UpdateProviderNodeFunc(ctx, id, payload)
	}
	return map[string]interface{}{"success": true}, nil
}

func (m *MockCoreClient) DeleteProviderNode(ctx context.Context, id string) error {
	if m.DeleteProviderNodeFunc != nil {
		return m.DeleteProviderNodeFunc(ctx, id)
	}
	return nil
}

func (m *MockCoreClient) ValidateProviderNode(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	if m.ValidateProviderNodeFunc != nil {
		return m.ValidateProviderNodeFunc(ctx, payload)
	}
	return map[string]interface{}{"valid": true}, nil
}

func (m *MockCoreClient) MitmStatus(ctx context.Context) (map[string]interface{}, error) {
	if m.MitmStatusFunc != nil {
		return m.MitmStatusFunc(ctx)
	}
	return map[string]interface{}{}, nil
}

func (m *MockCoreClient) MitmStart(ctx context.Context, apiKey string) (map[string]interface{}, error) {
	if m.MitmStartFunc != nil {
		return m.MitmStartFunc(ctx, apiKey)
	}
	return map[string]interface{}{"status": "started"}, nil
}

func (m *MockCoreClient) MitmStop(ctx context.Context) (map[string]interface{}, error) {
	if m.MitmStopFunc != nil {
		return m.MitmStopFunc(ctx)
	}
	return map[string]interface{}{"status": "stopped"}, nil
}

func (m *MockCoreClient) MitmDNS(ctx context.Context, tool, action string) (map[string]interface{}, error) {
	if m.MitmDNSFunc != nil {
		return m.MitmDNSFunc(ctx, tool, action)
	}
	return map[string]interface{}{"success": true}, nil
}

func (m *MockCoreClient) MitmAliasGet(ctx context.Context, tool string) (map[string]interface{}, error) {
	if m.MitmAliasGetFunc != nil {
		return m.MitmAliasGetFunc(ctx, tool)
	}
	return map[string]interface{}{}, nil
}

func (m *MockCoreClient) MitmAliasPut(ctx context.Context, tool string, mappings map[string]interface{}) (map[string]interface{}, error) {
	if m.MitmAliasPutFunc != nil {
		return m.MitmAliasPutFunc(ctx, tool, mappings)
	}
	return map[string]interface{}{"success": true}, nil
}

func (m *MockCoreClient) MCPRegistry(ctx context.Context) (map[string]interface{}, error) {
	if m.MCPRegistryFunc != nil {
		return m.MCPRegistryFunc(ctx)
	}
	return map[string]interface{}{}, nil
}

func (m *MockCoreClient) MCPInspect(ctx context.Context, url string) (map[string]interface{}, error) {
	if m.MCPInspectFunc != nil {
		return m.MCPInspectFunc(ctx, url)
	}
	return map[string]interface{}{}, nil
}

func (m *MockCoreClient) OAuthAuthorize(ctx context.Context, provider, redirectURI string) (map[string]interface{}, error) {
	if m.OAuthAuthorizeFunc != nil {
		return m.OAuthAuthorizeFunc(ctx, provider, redirectURI)
	}
	return map[string]interface{}{"authUrl": "https://example.com/oauth"}, nil
}

func (m *MockCoreClient) OAuthExchange(ctx context.Context, provider string, payload map[string]interface{}) (map[string]interface{}, error) {
	if m.OAuthExchangeFunc != nil {
		return m.OAuthExchangeFunc(ctx, provider, payload)
	}
	return map[string]interface{}{"success": true}, nil
}

func (m *MockCoreClient) KiroSocialAuthorize(ctx context.Context, idp, redirectURI string) (map[string]interface{}, error) {
	if m.KiroSocialAuthorizeFunc != nil {
		return m.KiroSocialAuthorizeFunc(ctx, idp, redirectURI)
	}
	return map[string]interface{}{"authUrl": "https://example.com/social"}, nil
}

func (m *MockCoreClient) KiroSocialExchange(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	if m.KiroSocialExchangeFunc != nil {
		return m.KiroSocialExchangeFunc(ctx, payload)
	}
	return map[string]interface{}{"success": true}, nil
}

func (m *MockCoreClient) MediaVoices(ctx context.Context, engine string) (map[string]interface{}, error) {
	if m.MediaVoicesFunc != nil {
		return m.MediaVoicesFunc(ctx, engine)
	}
	return map[string]interface{}{}, nil
}

func (m *MockCoreClient) PxpipeLogs(ctx context.Context) (map[string]interface{}, error) {
	if m.PxpipeLogsFunc != nil {
		return m.PxpipeLogsFunc(ctx)
	}
	return map[string]interface{}{}, nil
}

func (m *MockCoreClient) PxpipeHealth(ctx context.Context) (map[string]interface{}, error) {
	if m.PxpipeHealthFunc != nil {
		return m.PxpipeHealthFunc(ctx)
	}
	return map[string]interface{}{"status": "healthy"}, nil
}

func (m *MockCoreClient) TranslatorSend(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	if m.TranslatorSendFunc != nil {
		return m.TranslatorSendFunc(ctx, payload)
	}
	return map[string]interface{}{}, nil
}

func (m *MockCoreClient) CLIToolSettings(ctx context.Context, tool string) (map[string]interface{}, error) {
	if m.CLIToolSettingsFunc != nil {
		return m.CLIToolSettingsFunc(ctx, tool)
	}
	return map[string]interface{}{}, nil
}

func (m *MockCoreClient) Pricing(ctx context.Context) (map[string]interface{}, error) {
	if m.PricingFunc != nil {
		return m.PricingFunc(ctx)
	}
	return map[string]interface{}{}, nil
}

func (m *MockCoreClient) CoreVersion(ctx context.Context) (map[string]interface{}, error) {
	if m.CoreVersionFunc != nil {
		return m.CoreVersionFunc(ctx)
	}
	return map[string]interface{}{"version": "1.0.0"}, nil
}

func (m *MockCoreClient) CoreKeys(ctx context.Context) (map[string]interface{}, error) {
	if m.CoreKeysFunc != nil {
		return m.CoreKeysFunc(ctx)
	}
	return map[string]interface{}{}, nil
}

func (m *MockCoreClient) GetMergedModels(ctx context.Context) (string, []map[string]interface{}, error) {
	if m.GetMergedModelsFunc != nil {
		return m.GetMergedModelsFunc(ctx)
	}
	return "list", []map[string]interface{}{}, nil
}
