package v1

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Control-plane pages mirroring 9router Core: self-hosted Provider Nodes,
// MITM Bridge server control, and MCP server inspector. All admin-only.

// NodesPage renders the self-hosted provider nodes table.
func (h *Handler) NodesPage(w http.ResponseWriter, r *http.Request) {
	nodes, err := h.coreClient.GetProviderNodes(r.Context())
	if err != nil {
		nodes = nil
	}
	h.render(w, r, "nodes.html", "base.html", map[string]interface{}{
		"ActivePage": "nodes",
		"Nodes":      nodes,
	})
}

// APINodesCreate creates a provider node.
func (h *Handler) APINodesCreate(w http.ResponseWriter, r *http.Request) {
	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	res, err := h.coreClient.CreateProviderNode(r.Context(), payload)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, res)
}

// APINodesUpdate replaces a provider node (core requires full fields).
func (h *Handler) APINodesUpdate(w http.ResponseWriter, r *http.Request) {
	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	res, err := h.coreClient.UpdateProviderNode(r.Context(), chi.URLParam(r, "id"), payload)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, res)
}

// APINodesDelete removes a provider node.
func (h *Handler) APINodesDelete(w http.ResponseWriter, r *http.Request) {
	if err := h.coreClient.DeleteProviderNode(r.Context(), chi.URLParam(r, "id")); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"success": true})
}

// APINodesValidate dry-runs a node definition.
func (h *Handler) APINodesValidate(w http.ResponseWriter, r *http.Request) {
	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	res, err := h.coreClient.ValidateProviderNode(r.Context(), payload)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, res)
}

// MitmPage renders the MITM bridge control panel (status loads via API).
func (h *Handler) MitmPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "mitm.html", "base.html", map[string]interface{}{
		"ActivePage": "mitm",
	})
}

// APIMitmStatus proxies MITM bridge status.
func (h *Handler) APIMitmStatus(w http.ResponseWriter, r *http.Request) {
	res, err := h.coreClient.MitmStatus(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, res)
}

// APIMitmStart starts the MITM bridge server.
func (h *Handler) APIMitmStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		APIKey string `json:"apiKey"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.APIKey == "" {
		req.APIKey = "sk_9router"
	}
	res, err := h.coreClient.MitmStart(r.Context(), req.APIKey)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, res)
}

// APIMitmStop stops the MITM bridge server.
func (h *Handler) APIMitmStop(w http.ResponseWriter, r *http.Request) {
	res, err := h.coreClient.MitmStop(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, res)
}

// APIMitmDNS toggles per-tool DNS interception (enable/disable only).
func (h *Handler) APIMitmDNS(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Tool   string `json:"tool"`
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.Action != "enable" && req.Action != "disable" {
		http.Error(w, "action must be enable or disable", http.StatusBadRequest)
		return
	}
	res, err := h.coreClient.MitmDNS(r.Context(), req.Tool, req.Action)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, res)
}

// MCPPage renders the MCP server registry + inspector.
func (h *Handler) MCPPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "mcp.html", "base.html", map[string]interface{}{
		"ActivePage": "mcp",
	})
}

// APIMCPRegistry proxies the cowork MCP server directory.
func (h *Handler) APIMCPRegistry(w http.ResponseWriter, r *http.Request) {
	res, err := h.coreClient.MCPRegistry(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, res)
}

// APIMCPInspect lists tools of an MCP server URL.
func (h *Handler) APIMCPInspect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" {
		http.Error(w, "url required", http.StatusBadRequest)
		return
	}
	res, err := h.coreClient.MCPInspect(r.Context(), req.URL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, res)
}
