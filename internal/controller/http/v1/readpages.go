package v1

import (
	"fmt"
	"net/http"
	"strings"

	"9router-gateway/internal/entity"
)

// Read-mostly pages mirroring 9router Core dashboard views:
// Agent Skills, Endpoint hub, own Profile, Quota overview,
// server Console Log viewer, and Usage analytics.

// SkillsPage renders shareable agent-skill snippets (gateway endpoint + key + model).
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

	h.render(w, r, "skills.html", "base.html", map[string]interface{}{
		"ActivePage":     "skills",
		"CurrentBaseURL": h.deriveCurrentBaseURL(r),
		"UserKey":        userKey,
		"UserModel":      userModel,
	})
}

// EndpointPage renders the gateway endpoint configuration hub.
func (h *Handler) EndpointPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "endpoint.html", "base.html", map[string]interface{}{
		"ActivePage":     "endpoint",
		"CurrentBaseURL": h.deriveCurrentBaseURL(r),
		"UpstreamURL":    h.cfg.GetUpstreamURL(),
	})
}

// ProfilePage renders the current user's own account overview.
func (h *Handler) ProfilePage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)

	keyCount := 0
	var userModel string
	if currentUser != nil {
		if keys, err := h.repo.GetAPIKeysByUserID(ctx, currentUser.ID); err == nil {
			keyCount = len(keys)
		}
		allowed := entity.ParseAllowedModels(currentUser.AllowedModels)
		if len(allowed) > 0 && allowed[0] != "*" {
			userModel = allowed[0]
		}
	}

	h.render(w, r, "profile.html", "base.html", map[string]interface{}{
		"ActivePage":  "profile",
		"ProfileUser": currentUser,
		"KeyCount":    keyCount,
		"UserModel":   userModel,
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
