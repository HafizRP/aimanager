package v1

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"9router-gateway/internal/upstream"
)

// ProxyPoolStats aggregates status metrics for configured proxy pools.
type ProxyPoolStats struct {
	Total    int `json:"total"`
	Active   int `json:"active"`
	Failed   int `json:"failed"`
	Untested int `json:"untested"`
	HTTP     int `json:"http"`
	SOCKS    int `json:"socks"`
}

// computeProxyPoolStats calculates high-level KPI metrics from proxy pools.
func computeProxyPoolStats(pools []upstream.ProxyPool) ProxyPoolStats {
	stats := ProxyPoolStats{Total: len(pools)}
	for _, p := range pools {
		status := strings.ToLower(p.TestStatus)
		if status == "active" || status == "ok" || status == "success" {
			stats.Active++
		} else if status == "failed" || status == "error" || status == "timeout" {
			stats.Failed++
		} else {
			stats.Untested++
		}

		proto := p.Protocol()
		if strings.HasPrefix(proto, "SOCKS") {
			stats.SOCKS++
		} else {
			stats.HTTP++
		}
	}
	return stats
}

// ProxyPoolsPage renders the proxy pool management page.
func (h *Handler) ProxyPoolsPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pools, err := h.coreClient.GetProxyPools(ctx)
	if err != nil {
		pools = nil
	}

	stats := computeProxyPoolStats(pools)

	h.render(w, r, "proxy_pools.html", "base.html", map[string]interface{}{
		"ActivePage": "proxy-pools",
		"ProxyPools": pools,
		"Pools":      pools,
		"Stats":      stats,
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

// APIProxyPoolsExport exports configured proxy pools as CSV or JSON.
func (h *Handler) APIProxyPoolsExport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pools, err := h.coreClient.GetProxyPools(ctx)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to fetch proxy pools: %v", err), http.StatusInternalServerError)
		return
	}

	format := strings.ToLower(r.URL.Query().Get("format"))
	if format == "" {
		format = "csv"
	}

	timestamp := time.Now().Format("20060102_150405")

	if format == "json" {
		type exportItem struct {
			ID         string `json:"id"`
			Target     string `json:"target"`
			Protocol   string `json:"protocol"`
			TestStatus string `json:"test_status"`
			IsActive   bool   `json:"is_active"`
			CreatedAt  string `json:"created_at"`
			UpdatedAt  string `json:"updated_at"`
		}
		items := make([]exportItem, len(pools))
		for i, p := range pools {
			items[i] = exportItem{
				ID:         p.ID,
				Target:     p.DataString(),
				Protocol:   p.Protocol(),
				TestStatus: p.TestStatus,
				IsActive:   p.IsActive,
				CreatedAt:  p.CreatedAt,
				UpdatedAt:  p.UpdatedAt,
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=proxy_pools_%s.json", timestamp))
		_ = json.NewEncoder(w).Encode(items)
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=proxy_pools_%s.csv", timestamp))

	writer := csv.NewWriter(w)
	defer writer.Flush()

	_ = writer.Write([]string{"ID", "Target", "Protocol", "TestStatus", "IsActive", "CreatedAt", "UpdatedAt"})

	for _, p := range pools {
		isActiveStr := "false"
		if p.IsActive {
			isActiveStr = "true"
		}
		testStatus := p.TestStatus
		if testStatus == "" {
			testStatus = "untested"
		}

		_ = writer.Write([]string{
			p.ID,
			p.DataString(),
			p.Protocol(),
			testStatus,
			isActiveStr,
			p.CreatedAt,
			p.UpdatedAt,
		})
	}
}
