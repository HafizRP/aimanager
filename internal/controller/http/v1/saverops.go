package v1

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Service operation proxies for 9router Core managed services
// (Headroom compressor, PXPipe multimodal compressor, Format Translator).
// All routes are admin-only; the service/action names are allow-listed below
// so callers cannot reach arbitrary core endpoints.

// allowedServiceActions bounds which core service actions the UI may invoke.
var allowedServiceActions = map[string]map[string]bool{
	"headroom": {"start": true, "stop": true, "restart": true},
	"pxpipe":   {"install": true, "start": true, "stop": true, "restart": true},
}

// writeJSON encodes v as JSON with the correct content type.
func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// APIServiceStatus proxies GET /api/{service}/status (headroom, pxpipe).
func (h *Handler) APIServiceStatus(w http.ResponseWriter, r *http.Request) {
	service := chi.URLParam(r, "service")
	if _, ok := allowedServiceActions[service]; !ok {
		http.Error(w, "unknown service", http.StatusBadRequest)
		return
	}
	res, err := h.coreClient.ServiceStatus(r.Context(), service)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, res)
}

// APIServiceStats proxies GET /api/{service}/stats (pxpipe).
func (h *Handler) APIServiceStats(w http.ResponseWriter, r *http.Request) {
	service := chi.URLParam(r, "service")
	if service != "pxpipe" {
		http.Error(w, "stats only available for pxpipe", http.StatusBadRequest)
		return
	}
	res, err := h.coreClient.ServiceStats(r.Context(), service)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, res)
}

// APIServiceAction proxies POST /api/{service}/{action} (start/stop/restart/install).
func (h *Handler) APIServiceAction(w http.ResponseWriter, r *http.Request) {
	service := chi.URLParam(r, "service")
	action := chi.URLParam(r, "action")
	actions, ok := allowedServiceActions[service]
	if !ok || !actions[action] {
		http.Error(w, "action not allowed", http.StatusBadRequest)
		return
	}
	var payload map[string]interface{}
	_ = json.NewDecoder(r.Body).Decode(&payload)
	res, err := h.coreClient.ServiceAction(r.Context(), service, action, payload)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if res == nil {
		res = map[string]interface{}{"success": true}
	}
	writeJSON(w, res)
}

// APITranslatorTranslate proxies POST /api/translator/translate (format test box).
func (h *Handler) APITranslatorTranslate(w http.ResponseWriter, r *http.Request) {
	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	res, err := h.coreClient.TranslatorTranslate(r.Context(), payload)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, res)
}
