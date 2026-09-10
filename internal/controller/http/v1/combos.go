package v1

import (
	"encoding/json"
	"net/http"
)

// CombosPage renders the provider combo/fallback management page.
func (h *Handler) CombosPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	combos, err := h.coreClient.GetCombos(ctx)
	if err != nil {
		combos = nil
	}

	models, _ := h.fetchUpstreamModels(ctx)

	h.render(w, r, "combos.html", "base.html", map[string]interface{}{
		"ActivePage": "combos",
		"Combos":     combos,
		"Models":     models,
	})
}

// APICombosCreate creates a provider combo in 9router Core.
func (h *Handler) APICombosCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req struct {
		Name   string   `json:"name"`
		Models []string `json:"models"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || len(req.Models) == 0 {
		http.Error(w, "name and at least one model required", http.StatusBadRequest)
		return
	}

	if err := h.coreClient.CreateCombo(ctx, req.Name, req.Models); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// APICombosUpdate updates an existing provider combo.
func (h *Handler) APICombosUpdate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req struct {
		ID     string   `json:"id"`
		Name   string   `json:"name"`
		Models []string `json:"models"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" || req.Name == "" || len(req.Models) == 0 {
		http.Error(w, "id, name, and models required", http.StatusBadRequest)
		return
	}

	if err := h.coreClient.UpdateCombo(ctx, req.ID, req.Name, req.Models); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// APICombosDelete deletes a provider combo.
func (h *Handler) APICombosDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.URL.Query().Get("id")
	if id == "" {
		var req struct {
			ID string `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		id = req.ID
	}
	if id == "" {
		http.Error(w, "combo id required", http.StatusBadRequest)
		return
	}

	if err := h.coreClient.DeleteCombo(ctx, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
}
