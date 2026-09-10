package v1

import (
	"encoding/json"
	"net/http"
)

// TokenSaverPage renders the token saver settings page.
func (h *Handler) TokenSaverPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	settings, err := h.coreClient.GetSettings(ctx)
	if err != nil {
		settings = make(map[string]interface{})
	}

	h.render(w, r, "token_saver.html", "base.html", map[string]interface{}{
		"ActivePage": "token-saver",
		"Settings":   settings,
	})
}

// APITokenSaverSave persists token saver settings to 9router Core.
func (h *Handler) APITokenSaverSave(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	if err := h.coreClient.UpdateSettings(ctx, payload); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
}
