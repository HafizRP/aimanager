package v1

import (
	"encoding/json"
	"net/http"

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
