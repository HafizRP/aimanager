package v1

import (
	"encoding/json"
	"net/http"
)

// APIModelAliasesGet returns the current model alias map as JSON.
func (h *Handler) APIModelAliasesGet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	aliases, err := h.coreClient.GetModelAliases(ctx)
	if err != nil {
		aliases = make(map[string]string)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"aliases": aliases})
}

// APIModelAliasSet creates or updates a model alias in 9router Core.
func (h *Handler) APIModelAliasSet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req struct {
		Alias string `json:"alias"`
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Alias == "" || req.Model == "" {
		http.Error(w, "alias and model required", http.StatusBadRequest)
		return
	}

	if err := h.coreClient.SetModelAlias(ctx, req.Alias, req.Model); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// APIModelAliasDelete removes a model alias from 9router Core.
func (h *Handler) APIModelAliasDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	alias := r.URL.Query().Get("alias")
	if alias == "" {
		var req struct {
			Alias string `json:"alias"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		alias = req.Alias
	}
	if alias == "" {
		http.Error(w, "alias required", http.StatusBadRequest)
		return
	}

	if err := h.coreClient.DeleteModelAlias(ctx, alias); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
}
