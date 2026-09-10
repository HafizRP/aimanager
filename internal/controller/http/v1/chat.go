package v1

import (
	"9router-gateway/internal/entity"
	"net/http"
)

func (h *Handler) ChatPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)

	// Fetch models
	allModels, _ := h.fetchUpstreamModels(ctx)
	combos, _ := h.coreClient.GetCombos(ctx)

	// Filter models by whitelist if standard user
	var availableModels []UpstreamModelItem
	if currentUser != nil && !currentUser.IsAdmin() {
		allowed := entity.ParseAllowedModels(currentUser.AllowedModels)
		isWildcard := len(allowed) == 1 && allowed[0] == "*"
		for _, m := range allModels {
			match := isWildcard
			if !match {
				for _, a := range allowed {
					if a == m.ID {
						match = true
						break
					}
				}
			}
			if match {
				availableModels = append(availableModels, m)
			}
		}
	} else {
		availableModels = allModels
	}

	// Fetch active user API key for playground requests
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

	currentBaseURL := h.deriveCurrentBaseURL(r)

	h.render(w, r, "chat.html", "base.html", map[string]interface{}{
		"ActivePage":     "chat",
		"Models":         availableModels,
		"Combos":         combos,
		"UserKey":        userKey,
		"CurrentBaseURL": currentBaseURL,
	})
}
