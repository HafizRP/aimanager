package v1

import "9router-gateway/internal/config"

// configAdapter bridges *config.Config (fields for Midtrans, getters/setters for
// upstream) onto the SettingsConfig interface expected by the use case layer.
// This keeps the domain layer decoupled from the concrete config struct.
type configAdapter struct {
	cfg *config.Config
}

func (a *configAdapter) SetUpstreamURL(url string)        { a.cfg.SetUpstreamURL(url) }
func (a *configAdapter) GetUpstreamURL() string           { return a.cfg.GetUpstreamURL() }
func (a *configAdapter) SetUpstreamAPIKey(key string)     { a.cfg.SetUpstreamAPIKey(key) }
func (a *configAdapter) SetNineRouterDBPath(path string)  { a.cfg.SetNineRouterDBPath(path) }
func (a *configAdapter) GetNineRouterDBPath() string      { return a.cfg.GetNineRouterDBPath() }
func (a *configAdapter) GetUpstreamAPIKey() string        { return a.cfg.GetUpstreamAPIKey() }
func (a *configAdapter) MidtransServerKey() string        { return a.cfg.MidtransServerKey }
func (a *configAdapter) SetMidtransServerKey(key string)  { a.cfg.MidtransServerKey = key }
func (a *configAdapter) MidtransClientKey() string        { return a.cfg.MidtransClientKey }
func (a *configAdapter) SetMidtransClientKey(key string)  { a.cfg.MidtransClientKey = key }
func (a *configAdapter) MidtransMerchantID() string       { return a.cfg.MidtransMerchantID }
func (a *configAdapter) SetMidtransMerchantID(id string)  { a.cfg.MidtransMerchantID = id }
func (a *configAdapter) MidtransIsProduction() bool       { return a.cfg.MidtransIsProduction }
func (a *configAdapter) SetMidtransIsProduction(v bool)   { a.cfg.MidtransIsProduction = v }