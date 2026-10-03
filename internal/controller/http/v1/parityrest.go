package v1

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// Parity rest: OAuth connect, Media voices, PXPipe detail, Translator send,
// CLI settings generators, Pricing, Core system pages. All admin-only.

// ---------------------------------------------------------------- OAuth ---

var oauthProviders = []string{"antigravity", "kiro-google", "kiro-github"}

func isOAuthProvider(p string) bool {
	for _, v := range oauthProviders {
		if v == p {
			return true
		}
	}
	return false
}

// APIOAuthAuthorize starts an OAuth flow and returns {authUrl, state, ...}.
// Kiro goes through social-authorize with provider=google|github.
func (h *Handler) APIOAuthAuthorize(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	if !isOAuthProvider(provider) {
		http.Error(w, "unknown provider (antigravity, kiro-google, kiro-github)", http.StatusBadRequest)
		return
	}
	redirectURI := r.URL.Query().Get("redirect_uri")
	if redirectURI == "" {
		redirectURI = "http://localhost:443/callback"
	}
	var (
		res map[string]interface{}
		err error
	)
	switch provider {
	case "kiro-google":
		res, err = h.coreClient.KiroSocialAuthorize(r.Context(), "google", redirectURI)
	case "kiro-github":
		res, err = h.coreClient.KiroSocialAuthorize(r.Context(), "github", redirectURI)
	default:
		res, err = h.coreClient.OAuthAuthorize(r.Context(), provider, redirectURI)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	res["provider"] = provider
	writeJSON(w, res)
}

// APIOAuthExchange completes an OAuth flow with the pasted code/state.
func (h *Handler) APIOAuthExchange(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	if !isOAuthProvider(provider) {
		http.Error(w, "unknown provider", http.StatusBadRequest)
		return
	}
	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	var (
		res map[string]interface{}
		err error
	)
	switch provider {
	case "kiro-google", "kiro-github":
		res, err = h.coreClient.KiroSocialExchange(r.Context(), payload)
	default:
		res, err = h.coreClient.OAuthExchange(r.Context(), provider, payload)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, res)
}

// ---------------------------------------------------------------- Media ---

var mediaEngines = []string{"", "deepgram", "inworld", "elevenlabs", "minimax"}

// MediaPage renders the TTS voice browser (engine tabs + search).
func (h *Handler) MediaPage(w http.ResponseWriter, r *http.Request) {
	engine := r.URL.Query().Get("engine")
	voices := map[string]interface{}{}
	if res, err := h.coreClient.MediaVoices(r.Context(), engine); err == nil {
		voices = res
	}
	h.render(w, r, "media.html", "base.html", map[string]interface{}{
		"ActivePage": "media",
		"Engine":     engine,
		"Voices":     voices,
	})
}

// APIMediaVoicesGeneric returns the generic voice catalog.
func (h *Handler) APIMediaVoicesGeneric(w http.ResponseWriter, r *http.Request) {
	res, err := h.coreClient.MediaVoices(r.Context(), "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, res)
}

// APIMediaVoices returns the voice catalog for an engine.
func (h *Handler) APIMediaVoices(w http.ResponseWriter, r *http.Request) {
	res, err := h.coreClient.MediaVoices(r.Context(), chi.URLParam(r, "engine"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, res)
}

// --------------------------------------------------------------- PXPipe ---

// PxpipePage renders the full PXPipe detail: health checklist, windows, logs.
func (h *Handler) PxpipePage(w http.ResponseWriter, r *http.Request) {
	var status, health, logs map[string]interface{}
	if res, err := h.coreClient.ServiceStatus(r.Context(), "pxpipe"); err == nil {
		status = res
	}
	if res, err := h.coreClient.PxpipeHealth(r.Context()); err == nil {
		health = res
	}
	if res, err := h.coreClient.PxpipeLogs(r.Context()); err == nil {
		logs = res
	}
	h.render(w, r, "pxpipe.html", "base.html", map[string]interface{}{
		"ActivePage": "pxpipe",
		"Status":     status,
		"Health":     health,
		"Logs":       logs,
	})
}

// APIPxpipeLogs returns the npm install/setup log.
func (h *Handler) APIPxpipeLogs(w http.ResponseWriter, r *http.Request) {
	res, err := h.coreClient.PxpipeLogs(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, res)
}

// APIPxpipeHealth returns the readiness checklist.
func (h *Handler) APIPxpipeHealth(w http.ResponseWriter, r *http.Request) {
	res, err := h.coreClient.PxpipeHealth(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, res)
}

// ------------------------------------------------------------ Translator ---

// TranslatorPage renders the full translator: detect + translate + send.
func (h *Handler) TranslatorPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "translator.html", "base.html", map[string]interface{}{
		"ActivePage": "translator",
	})
}

// APITranslatorSend forwards a translate request through a provider+model.
func (h *Handler) APITranslatorSend(w http.ResponseWriter, r *http.Request) {
	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	res, err := h.coreClient.TranslatorSend(r.Context(), payload)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, res)
}

// ---------------------------------------------------------- CLI settings ---

var cliSettingsTools = []string{"claude", "codex", "opencode", "openclaw",
	"grok-build", "kilo", "devin", "droid", "deepseek-tui", "jcode",
	"cline", "copilot", "cowork", "hermes"}

// APICLIToolSettings proxies a tool's local settings/config readout.
func (h *Handler) APICLIToolSettings(w http.ResponseWriter, r *http.Request) {
	tool := chi.URLParam(r, "tool")
	allowed := false
	for _, t := range cliSettingsTools {
		if t == tool {
			allowed = true
			break
		}
	}
	if !allowed {
		http.Error(w, "unknown tool", http.StatusBadRequest)
		return
	}
	res, err := h.coreClient.CLIToolSettings(r.Context(), tool)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, res)
}

// ---------------------------------------------------------------- Pricing ---

// PricingPage renders the per-model price tables ($/1M tokens).
func (h *Handler) PricingPage(w http.ResponseWriter, r *http.Request) {
	pricing, err := h.coreClient.Pricing(r.Context())
	if err != nil {
		pricing = nil
	}
	h.render(w, r, "pricing.html", "base.html", map[string]interface{}{
		"ActivePage": "pricing",
		"Pricing":    pricing,
	})
}

// APIPricing returns the raw pricing payload.
func (h *Handler) APIPricing(w http.ResponseWriter, r *http.Request) {
	res, err := h.coreClient.Pricing(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, res)
}

// PricingEstimateRequest carries input parameters for token cost estimation.
type PricingEstimateRequest struct {
	Source              string  `json:"source"`
	Model               string  `json:"model"`
	PromptTokens        int     `json:"prompt_tokens"`
	CompletionTokens    int     `json:"completion_tokens"`
	CachedTokens        int     `json:"cached_tokens"`
	ReasoningTokens     int     `json:"reasoning_tokens"`
	Requests            int     `json:"requests"`
	USDToIDR            float64 `json:"usd_to_idr"`
	CustomInputRate     float64 `json:"custom_input_rate"`
	CustomOutputRate    float64 `json:"custom_output_rate"`
	CustomCachedRate    float64 `json:"custom_cached_rate"`
	CustomReasoningRate float64 `json:"custom_reasoning_rate"`
}

// PricingEstimateResponse carries calculated cost estimation and comparison.
type PricingEstimateResponse struct {
	Model      string                 `json:"model"`
	Source     string                 `json:"source"`
	Rates      map[string]float64     `json:"rates"`
	Tokens     map[string]interface{} `json:"tokens"`
	CostUSD    map[string]float64     `json:"cost_usd"`
	CostIDR    map[string]float64     `json:"cost_idr"`
	Comparison map[string]interface{} `json:"comparison"`
}

func parseRateFloat(v interface{}) float64 {
	if v == nil {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case string:
		f, _ := strconv.ParseFloat(n, 64)
		return f
	default:
		return 0
	}
}

// APIPricingEstimate calculates token costs in USD and IDR with ROI baseline comparison.
func (h *Handler) APIPricingEstimate(w http.ResponseWriter, r *http.Request) {
	var req PricingEstimateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	var inputRate, outputRate, cachedRate, reasoningRate float64
	sourceKey := req.Source
	if sourceKey == "" {
		sourceKey = "tokenrouter"
	}

	if req.Source == "custom" {
		inputRate = req.CustomInputRate
		outputRate = req.CustomOutputRate
		cachedRate = req.CustomCachedRate
		reasoningRate = req.CustomReasoningRate
	} else {
		pricing, err := h.coreClient.Pricing(r.Context())
		if err == nil && pricing != nil {
			if table, ok := pricing[sourceKey].(map[string]interface{}); ok {
				if modelRates, ok := table[req.Model].(map[string]interface{}); ok {
					inputRate = parseRateFloat(modelRates["input"])
					outputRate = parseRateFloat(modelRates["output"])
					cachedRate = parseRateFloat(modelRates["cached"])
					reasoningRate = parseRateFloat(modelRates["reasoning"])
				}
			}
		}
		// If model rate not found in catalog, fall back to custom rates if provided
		if inputRate == 0 && outputRate == 0 {
			if req.CustomInputRate > 0 || req.CustomOutputRate > 0 {
				inputRate = req.CustomInputRate
				outputRate = req.CustomOutputRate
				cachedRate = req.CustomCachedRate
				reasoningRate = req.CustomReasoningRate
			}
		}
	}

	reqCount := req.Requests
	if reqCount <= 0 {
		reqCount = 1
	}

	exchangeRate := req.USDToIDR
	if exchangeRate <= 0 {
		exchangeRate = 16000.0
	}

	promptTokens := req.PromptTokens
	if promptTokens < 0 {
		promptTokens = 0
	}
	completionTokens := req.CompletionTokens
	if completionTokens < 0 {
		completionTokens = 0
	}
	cachedTokens := req.CachedTokens
	if cachedTokens < 0 {
		cachedTokens = 0
	}
	reasoningTokens := req.ReasoningTokens
	if reasoningTokens < 0 {
		reasoningTokens = 0
	}

	// Cost per single request ($ USD) based on rate per 1M tokens ($ / 1,000,000)
	inputCostSingle := (float64(promptTokens) / 1000000.0) * inputRate
	outputCostSingle := (float64(completionTokens) / 1000000.0) * outputRate
	cachedCostSingle := (float64(cachedTokens) / 1000000.0) * cachedRate
	reasoningCostSingle := (float64(reasoningTokens) / 1000000.0) * reasoningRate

	costPerReqUSD := inputCostSingle + outputCostSingle + cachedCostSingle + reasoningCostSingle
	totalInputCostUSD := inputCostSingle * float64(reqCount)
	totalOutputCostUSD := outputCostSingle * float64(reqCount)
	totalCachedCostUSD := cachedCostSingle * float64(reqCount)
	totalReasoningCostUSD := reasoningCostSingle * float64(reqCount)
	totalCostUSD := costPerReqUSD * float64(reqCount)

	costPerReqIDR := costPerReqUSD * exchangeRate
	totalCostIDR := totalCostUSD * exchangeRate

	tokensPerReq := promptTokens + completionTokens + cachedTokens + reasoningTokens
	totalTokensAll := tokensPerReq * reqCount

	// Standard baseline benchmark: $5.00 per 1M tokens (average OpenAI GPT-4 tier)
	baselineRatePerM := 5.0
	baselinePerReqUSD := (float64(tokensPerReq) / 1000000.0) * baselineRatePerM
	baselineTotalUSD := baselinePerReqUSD * float64(reqCount)
	diffUSD := baselineTotalUSD - totalCostUSD
	savingsPct := 0.0
	if baselineTotalUSD > 0 {
		savingsPct = (diffUSD / baselineTotalUSD) * 100.0
	}

	resp := PricingEstimateResponse{
		Model:  req.Model,
		Source: sourceKey,
		Rates: map[string]float64{
			"input":     inputRate,
			"output":    outputRate,
			"cached":    cachedRate,
			"reasoning": reasoningRate,
		},
		Tokens: map[string]interface{}{
			"prompt_tokens":            promptTokens,
			"completion_tokens":        completionTokens,
			"cached_tokens":            cachedTokens,
			"reasoning_tokens":         reasoningTokens,
			"tokens_per_request":       tokensPerReq,
			"requests":                 reqCount,
			"total_tokens":             totalTokensAll,
			"total_prompt_tokens":      promptTokens * reqCount,
			"total_completion_tokens":  completionTokens * reqCount,
		},
		CostUSD: map[string]float64{
			"input_cost":       totalInputCostUSD,
			"output_cost":      totalOutputCostUSD,
			"cached_cost":      totalCachedCostUSD,
			"reasoning_cost":   totalReasoningCostUSD,
			"cost_per_request": costPerReqUSD,
			"total_cost":       totalCostUSD,
		},
		CostIDR: map[string]float64{
			"exchange_rate":    exchangeRate,
			"cost_per_request": costPerReqIDR,
			"total_cost":       totalCostIDR,
		},
		Comparison: map[string]interface{}{
			"baseline_rate_per_million": baselineRatePerM,
			"baseline_cost_per_request": baselinePerReqUSD,
			"baseline_total_usd":        baselineTotalUSD,
			"savings_usd":               diffUSD,
			"savings_pct":               savingsPct,
			"is_savings":                diffUSD >= 0,
		},
	}

	writeJSON(w, resp)
}

// ------------------------------------------------------------ Core system ---

// APICoreVersion returns core version + update availability.
func (h *Handler) APICoreVersion(w http.ResponseWriter, r *http.Request) {
	res, err := h.coreClient.CoreVersion(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, res)
}

// APICoreKeys returns machine API keys registered in core (admin-only).
func (h *Handler) APICoreKeys(w http.ResponseWriter, r *http.Request) {
	res, err := h.coreClient.CoreKeys(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, res)
}
