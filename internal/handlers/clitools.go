package handlers

import (
	"net/http"
)

func (h *Handler) CLIToolsPage(w http.ResponseWriter, r *http.Request) {
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

	userModel := "ag/gemini-3.8-flash-high"
	if currentUser != nil {
		allowed := parseAllowedModels(currentUser.AllowedModels)
		if len(allowed) > 0 && allowed[0] != "*" {
			userModel = allowed[0]
		}
	}

	currentBaseURL := h.deriveCurrentBaseURL(r)

	h.render(w, r, "cli_tools.html", "base.html", map[string]interface{}{
		"ActivePage":     "cli-tools",
		"CurrentBaseURL": currentBaseURL,
		"UserKey":        userKey,
		"UserModel":      userModel,
	})
}
