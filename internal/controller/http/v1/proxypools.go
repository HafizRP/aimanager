package v1

import (
	"encoding/json"
	"net/http"
)

// ProxyPoolsPage renders the proxy pool management page.
func (h *Handler) ProxyPoolsPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pools, err := h.coreClient.GetProxyPools(ctx)
	if err != nil {
		pools = nil
	}

	h.render(w, r, "proxy_pools.html", "base.html", map[string]interface{}{
		"ActivePage": "proxy-pools",
		"ProxyPools": pools,
	})
}

// APIProxyPoolsCreate creates a proxy pool in 9router Core.
func (h *Handler) APIProxyPoolsCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	if err := h.coreClient.CreateProxyPool(ctx, payload); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// APIProxyPoolsDelete deletes a proxy pool.
func (h *Handler) APIProxyPoolsDelete(w http.ResponseWriter, r *http.Request) {
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
		http.Error(w, "proxy pool id required", http.StatusBadRequest)
		return
	}

	if err := h.coreClient.DeleteProxyPool(ctx, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// APIProxyPoolsTest tests a proxy pool connection.
func (h *Handler) APIProxyPoolsTest(w http.ResponseWriter, r *http.Request) {
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
		http.Error(w, "proxy pool id required", http.StatusBadRequest)
		return
	}

	res, err := h.coreClient.TestProxyPool(ctx, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}
