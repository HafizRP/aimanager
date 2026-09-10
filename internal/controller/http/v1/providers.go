package v1

import (
	"encoding/json"
	"net/http"
)

// ProvidersPage renders the upstream provider management page.
func (h *Handler) ProvidersPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	conns, err := h.coreClient.GetProviders(ctx)
	if err != nil {
		conns = nil
	}

	h.render(w, r, "providers.html", "base.html", map[string]interface{}{
		"ActivePage": "providers",
		"Providers":  conns,
	})
}

// APIProvidersToggle enables/disables an upstream provider.
func (h *Handler) APIProvidersToggle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req struct {
		ID       string `json:"id"`
		IsActive bool   `json:"isActive"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	if err := h.coreClient.ToggleProvider(ctx, req.ID, req.IsActive); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// APIProvidersPriority reorders provider priority.
func (h *Handler) APIProvidersPriority(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req struct {
		ID       string `json:"id"`
		Priority int    `json:"priority"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	if err := h.coreClient.SetProviderPriority(ctx, req.ID, req.Priority); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// APIProvidersTest tests connectivity to an upstream provider.
func (h *Handler) APIProvidersTest(w http.ResponseWriter, r *http.Request) {
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
		http.Error(w, "provider id required", http.StatusBadRequest)
		return
	}

	res, err := h.coreClient.TestProvider(ctx, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

// APIProvidersDelete removes an upstream provider.
func (h *Handler) APIProvidersDelete(w http.ResponseWriter, r *http.Request) {
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
		http.Error(w, "provider id required", http.StatusBadRequest)
		return
	}

	if err := h.coreClient.DeleteProvider(ctx, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// APIProvidersCreate adds a new upstream provider.
func (h *Handler) APIProvidersCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid json payload", http.StatusBadRequest)
		return
	}

	res, err := h.coreClient.CreateProvider(ctx, payload)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}
