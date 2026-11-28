package v1

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"9router-gateway/internal/entity"
)

// Read-mostly pages mirroring 9router Core dashboard views:
// Agent Skills, Endpoint hub, own Profile, Quota overview,
// server Console Log viewer, and Usage analytics.

// SkillsPage renders shareable agent-skill snippets plus the skills registry.
func (h *Handler) SkillsPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)

	var userKey string
	if currentUser != nil {
		keys, err := h.repo.GetAPIKeysByUserID(ctx, currentUser.ID)
		if err == nil && len(keys) > 0 {
			for _, k := range keys {
				if k.IsActive {
					userKey = k.Key
					break
				}
			}
			if userKey == "" {
				userKey = keys[0].Key
			}
		}
	}

	userModel := "main"
	if currentUser != nil {
		allowed := entity.ParseAllowedModels(currentUser.AllowedModels)
		if len(allowed) > 0 && allowed[0] != "*" {
			userModel = allowed[0]
		}
	}

	search := strings.TrimSpace(r.URL.Query().Get("q"))
	skills, err := h.repo.ListSkills(ctx, search)
	if err != nil {
		skills = nil
	}

	isAdmin := currentUser != nil && currentUser.IsAdmin()

	h.render(w, r, "skills.html", "base.html", map[string]interface{}{
		"ActivePage":     "skills",
		"CurrentBaseURL": h.deriveCurrentBaseURL(r),
		"UserKey":        userKey,
		"UserModel":      userModel,
		"Skills":         skills,
		"Search":         search,
		"IsAdmin":        isAdmin,
	})
}

// EndpointItem describes an HTTP entrypoint exposed by AI Manager Gateway or its upstream.
type EndpointItem struct {
	Name      string `json:"name"`
	Method    string `json:"method"`
	Path      string `json:"path"`
	URL       string `json:"url"`
	Purpose   string `json:"purpose"`
	Category  string `json:"category"`
	IsProbing bool   `json:"is_probing"`
}

// getGatewayEndpoints returns the structured endpoint catalog for display and export.
func getGatewayEndpoints(baseURL, upstreamURL string) []EndpointItem {
	return []EndpointItem{
		{
			Name:      "OpenAI Chat Completions",
			Method:    "POST",
			Path:      "/v1/chat/completions",
			URL:       baseURL + "/v1/chat/completions",
			Purpose:   "OpenAI-compatible chat completions with streaming & thinking options",
			Category:  "Inference",
			IsProbing: false,
		},
		{
			Name:      "OpenAI Models Catalog",
			Method:    "GET",
			Path:      "/v1/models",
			URL:       baseURL + "/v1/models",
			Purpose:   "List available models, combos, and custom upstream nodes",
			Category:  "Inference",
			IsProbing: true,
		},
		{
			Name:      "Anthropic Messages API",
			Method:    "POST",
			Path:      "/v1/messages",
			URL:       baseURL + "/v1/messages",
			Purpose:   "Anthropic Messages protocol compatible endpoint",
			Category:  "Inference",
			IsProbing: false,
		},
		{
			Name:      "Text Embeddings",
			Method:    "POST",
			Path:      "/v1/embeddings",
			URL:       baseURL + "/v1/embeddings",
			Purpose:   "OpenAI-compatible vector text embeddings",
			Category:  "Inference",
			IsProbing: false,
		},
		{
			Name:      "OpenAPI 3.1 Specification",
			Method:    "GET",
			Path:      "/openapi.json",
			URL:       baseURL + "/openapi.json",
			Purpose:   "Machine-readable API specification for Postman, Insomnia, or Swagger UI",
			Category:  "Documentation",
			IsProbing: true,
		},
		{
			Name:      "Liveness Probe (Health)",
			Method:    "GET",
			Path:      "/healthz",
			URL:       baseURL + "/healthz",
			Purpose:   "Kubernetes and Docker liveness check (returns 200 OK)",
			Category:  "Monitoring",
			IsProbing: true,
		},
		{
			Name:      "Readiness Probe (DB Ping)",
			Method:    "GET",
			Path:      "/readyz",
			URL:       baseURL + "/readyz",
			Purpose:   "Readiness check verifying database connection availability",
			Category:  "Monitoring",
			IsProbing: true,
		},
		{
			Name:      "Service Version & Info",
			Method:    "GET",
			Path:      "/version",
			URL:       baseURL + "/version",
			Purpose:   "Service metadata, version string, and application status",
			Category:  "Monitoring",
			IsProbing: true,
		},
		{
			Name:      "Upstream 9router Core",
			Method:    "ANY",
			Path:      "/",
			URL:       upstreamURL,
			Purpose:   "Internal 9router Core engine (strictly loopback bound)",
			Category:  "Upstream",
			IsProbing: false,
		},
	}
}

// EndpointPage renders the gateway endpoint configuration hub.
func (h *Handler) EndpointPage(w http.ResponseWriter, r *http.Request) {
	baseURL := h.deriveCurrentBaseURL(r)
	upstreamURL := h.cfg.GetUpstreamURL()
	endpoints := getGatewayEndpoints(baseURL, upstreamURL)

	h.render(w, r, "endpoint.html", "base.html", map[string]interface{}{
		"ActivePage":     "endpoint",
		"CurrentBaseURL": baseURL,
		"UpstreamURL":    upstreamURL,
		"Endpoints":      endpoints,
	})
}

// APIEndpointsExport exports the gateway endpoint catalog as CSV or JSON.
func (h *Handler) APIEndpointsExport(w http.ResponseWriter, r *http.Request) {
	baseURL := h.deriveCurrentBaseURL(r)
	upstreamURL := h.cfg.GetUpstreamURL()
	endpoints := getGatewayEndpoints(baseURL, upstreamURL)

	format := strings.ToLower(r.URL.Query().Get("format"))
	if format == "" {
		format = "csv"
	}

	timestamp := time.Now().Format("20060102_150405")

	if format == "json" {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=endpoints_%s.json", timestamp))
		_ = json.NewEncoder(w).Encode(endpoints)
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=endpoints_%s.csv", timestamp))

	writer := csv.NewWriter(w)
	defer writer.Flush()

	_ = writer.Write([]string{"Name", "Method", "Path", "URL", "Category", "Purpose"})

	for _, ep := range endpoints {
		_ = writer.Write([]string{
			ep.Name,
			ep.Method,
			ep.Path,
			ep.URL,
			ep.Category,
			ep.Purpose,
		})
	}
}

// ProfilePage renders the current user's own account overview.
func (h *Handler) ProfilePage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)

	keyCount := 0
	var userModel string
	var loginAudits []entity.LoginAudit
	if currentUser != nil {
		if keys, err := h.repo.GetAPIKeysByUserID(ctx, currentUser.ID); err == nil {
			keyCount = len(keys)
		}
		allowed := entity.ParseAllowedModels(currentUser.AllowedModels)
		if len(allowed) > 0 && allowed[0] != "*" {
			userModel = allowed[0]
		}
		if audits, err := h.repo.GetUserLoginAudits(ctx, currentUser.ID, 10); err == nil {
			loginAudits = audits
		}
	}

	h.render(w, r, "profile.html", "base.html", map[string]interface{}{
		"ActivePage":  "profile",
		"ProfileUser": currentUser,
		"KeyCount":    keyCount,
		"UserModel":   userModel,
		"LoginAudits": loginAudits,
		"SuccessMsg":  r.URL.Query().Get("msg"),
		"ErrorMsg":    r.URL.Query().Get("error"),
	})
}

// QuotaPage renders the upstream quota overview (data loads via /api/upstream/quotas).
func (h *Handler) QuotaPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "quota.html", "base.html", map[string]interface{}{
		"ActivePage": "quota",
	})
}

// ConsoleLogPage renders the 9router Core server console log viewer.
func (h *Handler) ConsoleLogPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "console_log.html", "base.html", map[string]interface{}{
		"ActivePage": "console-log",
	})
}

// APIConsoleLogs proxies core console logs as JSON.
func (h *Handler) APIConsoleLogs(w http.ResponseWriter, r *http.Request) {
	res, err := h.coreClient.ConsoleLogs(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, res)
}

// UsagePage renders upstream usage analytics (data loads via /api/usage/stats).
func (h *Handler) UsagePage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "usage.html", "base.html", map[string]interface{}{
		"ActivePage": "usage",
	})
}

// APIUsageStats proxies core usage analytics as JSON.
func (h *Handler) APIUsageStats(w http.ResponseWriter, r *http.Request) {
	res, err := h.coreClient.UsageStats(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if user := GetUserFromContext(r.Context()); user == nil || !user.IsAdmin() {
		scrubUsageAccounts(res)
	}
	writeJSON(w, res)
}

// scrubUsageAccounts replaces account identities in byAccount keys
// ("model (provider - email)") with per-provider labels for non-admin viewers.
func scrubUsageAccounts(res map[string]interface{}) {
	raw, ok := res["byAccount"].(map[string]interface{})
	if !ok {
		return
	}
	counters := map[string]int{}
	labels := map[string]string{}
	scrubbed := make(map[string]interface{}, len(raw))
	for key, val := range raw {
		model, provider, account := splitUsageAccountKey(key)
		if !strings.Contains(account, "@") {
			scrubbed[key] = val
			continue
		}
		ck := provider + "\x00" + account
		label, ok := labels[ck]
		if !ok {
			counters[provider]++
			label = fmt.Sprintf("Account %d", counters[provider])
			labels[ck] = label
		}
		scrubbed[fmt.Sprintf("%s (%s - %s)", model, provider, label)] = val
	}
	res["byAccount"] = scrubbed
}

// splitUsageAccountKey splits "model (provider - account)" into parts.
func splitUsageAccountKey(key string) (model, provider, account string) {
	open := strings.LastIndex(key, " (")
	if open < 0 || !strings.HasSuffix(key, ")") {
		return key, "", ""
	}
	model = key[:open]
	inner := key[open+2 : len(key)-1]
	parts := strings.SplitN(inner, " - ", 2)
	if len(parts) != 2 {
		return model, inner, ""
	}
	return model, parts[0], parts[1]
}
