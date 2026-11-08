package v1

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ReplayResult holds the outcome of replaying one stored prompt against one model.
type ReplayResult struct {
	Model            string `json:"model"`
	OutputText       string `json:"output_text"`
	LatencyMs        int64  `json:"latency_ms"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
	StatusCode       int    `json:"status_code"`
	Success          bool   `json:"success"`
	ErrorMsg         string `json:"error_msg,omitempty"`
}

// ReplayPage renders the Request Replay Lab page.
// Optional ?log_id= preselects a stored request as the replay source.
func (h *Handler) ReplayPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	models, _ := h.fetchUpstreamModels(ctx)

	h.render(w, r, "replay.html", "base.html", map[string]interface{}{
		"ActivePage": "replay",
		"Models":     models,
		"PreselectLogID": r.URL.Query().Get("log_id"),
	})
}

// APIReplaySource returns a single stored log (prompt + original response)
// for the replay lab. Non-admin users can only fetch their own logs.
func (h *Handler) APIReplaySource(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)

	var id int64
	if _, err := fmt.Sscanf(strings.TrimSpace(r.URL.Query().Get("id")), "%d", &id); err != nil || id <= 0 {
		http.Error(w, "invalid log id", http.StatusBadRequest)
		return
	}

	l, err := h.repo.GetRequestLogByID(ctx, id)
	if err != nil || l == nil {
		http.Error(w, "log not found", http.StatusNotFound)
		return
	}
	if currentUser != nil && !currentUser.IsAdmin() && l.UserID != currentUser.ID {
		http.Error(w, "log not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"id":                l.ID,
		"user_id":           l.UserID,
		"user_name":         l.UserName,
		"key_name":          l.KeyName,
		"model":             l.Model,
		"path":              l.Path,
		"method":            l.Method,
		"is_stream":         l.IsStream,
		"prompt_tokens":     l.PromptTokens,
		"completion_tokens": l.CompletionTokens,
		"total_tokens":      l.TotalTokens,
		"duration_ms":       l.DurationMs,
		"client_ip":         l.ClientIP,
		"error_message":     l.ErrorMessage,
		"request_body":      l.RequestBody,
		"response_text":     l.ResponseText,
		"status_code":       l.StatusCode,
		"created_at":        l.CreatedAt,
	})
}

// APIReplayRun replays a stored prompt against up to 4 models in parallel
// through the gateway itself (server-side, using the caller's own API key),
// and returns each model's output, latency, and token usage side-by-side.
func (h *Handler) APIReplayRun(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)

	var req struct {
		LogID       int64    `json:"log_id"`
		Models      []string `json:"models"`
		MaxTokens   int      `json:"max_tokens"`
		Temperature *float64 `json:"temperature"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if len(req.Models) == 0 {
		http.Error(w, "no models selected", http.StatusBadRequest)
		return
	}
	if len(req.Models) > 4 {
		req.Models = req.Models[:4]
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 || maxTokens > 1024 {
		maxTokens = 512
	}

	// Load the stored prompt.
	src, err := h.repo.GetRequestLogByID(ctx, req.LogID)
	if err != nil || src == nil {
		http.Error(w, "log not found", http.StatusNotFound)
		return
	}
	if currentUser != nil && !currentUser.IsAdmin() && src.UserID != currentUser.ID {
		http.Error(w, "log not found", http.StatusNotFound)
		return
	}
	if src.RequestBody == "" {
		http.Error(w, "no stored prompt for this log (only new requests carry replayable payloads)", http.StatusUnprocessableEntity)
		return
	}
	if !strings.Contains(src.Path, "chat/completions") {
		http.Error(w, "only chat-completions requests can be replayed", http.StatusUnprocessableEntity)
		return
	}
	var basePayload map[string]interface{}
	if err := json.Unmarshal([]byte(src.RequestBody), &basePayload); err != nil {
		http.Error(w, "stored prompt is not valid JSON", http.StatusUnprocessableEntity)
		return
	}
	if _, ok := basePayload["messages"]; !ok {
		http.Error(w, "stored prompt has no messages array", http.StatusUnprocessableEntity)
		return
	}

	// Resolve the caller's own active key (same pattern as benchmark).
	var userKey string
	if currentUser != nil {
		if keys, err := h.repo.GetAPIKeysByUserID(ctx, currentUser.ID); err == nil {
			for _, k := range keys {
				if k.IsActive {
					userKey = k.Key
					break
				}
			}
		}
	}
	if userKey == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "No active API key found for replay"})
		return
	}

	// Model access guard per target model.
	var allowed []string
	for _, m := range req.Models {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		if currentUser != nil && !currentUser.HasModelAccess(m) {
			continue
		}
		allowed = append(allowed, m)
	}
	if len(allowed) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "No selected model is allowed for your account"})
		return
	}

	var wg sync.WaitGroup
	results := make([]ReplayResult, len(allowed))
	for i, m := range allowed {
		wg.Add(1)
		go func(idx int, modelName string) {
			defer wg.Done()
			results[idx] = h.replaySingleModel(src.Path, basePayload, modelName, userKey, maxTokens, req.Temperature)
		}(i, m)
	}
	wg.Wait()

	sort.Slice(results, func(i, j int) bool {
		if results[i].Success != results[j].Success {
			return results[i].Success
		}
		return results[i].LatencyMs < results[j].LatencyMs
	})

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"source": map[string]interface{}{
			"log_id": src.ID, "model": src.Model, "response_text": src.ResponseText,
		},
		"results": results,
	})
}

func (h *Handler) replaySingleModel(path string, basePayload map[string]interface{}, modelName, apiKey string, maxTokens int, temperature *float64) ReplayResult {
	start := time.Now()
	res := ReplayResult{Model: modelName}

	payload := make(map[string]interface{}, len(basePayload)+3)
	for k, v := range basePayload {
		payload[k] = v
	}
	payload["model"] = modelName
	payload["stream"] = false
	payload["max_tokens"] = maxTokens
	if temperature != nil {
		payload["temperature"] = *temperature
	}

	b, _ := json.Marshal(payload)
	targetURL := fmt.Sprintf("http://127.0.0.1:%d%s", h.cfg.Port, path)

	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequestWithContext(context.Background(), "POST", targetURL, bytes.NewReader(b))
	if err != nil {
		res.ErrorMsg = err.Error()
		return res
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	res.LatencyMs = time.Since(start).Milliseconds()
	if err != nil {
		res.ErrorMsg = err.Error()
		return res
	}
	defer resp.Body.Close()
	res.StatusCode = resp.StatusCode

	if resp.StatusCode != http.StatusOK {
		var errBody struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errBody)
		res.ErrorMsg = errBody.Error.Message
		if res.ErrorMsg == "" {
			res.ErrorMsg = fmt.Sprintf("Upstream HTTP %d", resp.StatusCode)
		}
		return res
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		res.ErrorMsg = "failed to decode upstream response: " + err.Error()
		return res
	}
	res.Success = true
	if len(parsed.Choices) > 0 {
		res.OutputText = parsed.Choices[0].Message.Content
		if res.OutputText == "" {
			res.OutputText = parsed.Choices[0].Message.ReasoningContent
		}
	}
	res.PromptTokens = parsed.Usage.PromptTokens
	res.CompletionTokens = parsed.Usage.CompletionTokens
	res.TotalTokens = parsed.Usage.TotalTokens
	return res
}

// APIReplayExport exports replay comparison results to Markdown, JSON, or CSV.
func (h *Handler) APIReplayExport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Format  string         `json:"format"`
		LogID   int64          `json:"log_id"`
		Prompt  string         `json:"prompt"`
		Results []ReplayResult `json:"results"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	format := strings.ToLower(strings.TrimSpace(req.Format))
	if format == "" {
		format = "markdown"
	}

	timestamp := time.Now().Format("20060102_150405")

	switch format {
	case "json":
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=replay_results_%s.json", timestamp))
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"exported_at": time.Now().UTC().Format(time.RFC3339),
			"log_id":      req.LogID,
			"prompt":      req.Prompt,
			"total_count": len(req.Results),
			"results":     req.Results,
		})

	case "csv":
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=replay_results_%s.csv", timestamp))
		writer := csv.NewWriter(w)
		defer writer.Flush()

		_ = writer.Write([]string{
			"Model", "Latency_MS", "Prompt_Tokens", "Completion_Tokens", "Total_Tokens", "Status", "Success", "Output", "Error",
		})
		for _, res := range req.Results {
			statusStr := "OK"
			if !res.Success {
				statusStr = fmt.Sprintf("HTTP %d", res.StatusCode)
			}
			_ = writer.Write([]string{
				res.Model,
				strconv.FormatInt(res.LatencyMs, 10),
				strconv.Itoa(res.PromptTokens),
				strconv.Itoa(res.CompletionTokens),
				strconv.Itoa(res.TotalTokens),
				statusStr,
				strconv.FormatBool(res.Success),
				res.OutputText,
				res.ErrorMsg,
			})
		}

	default: // markdown
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=replay_results_%s.md", timestamp))
		var b strings.Builder
		fmt.Fprintf(&b, "# Request Replay Lab Comparison Report\n\n")
		fmt.Fprintf(&b, "- **Source Log ID**: `%d`\n", req.LogID)
		fmt.Fprintf(&b, "- **Exported At**: `%s`\n", time.Now().Format("2006-01-02 15:04:05 MST"))
		fmt.Fprintf(&b, "- **Models Tested**: `%d`\n\n", len(req.Results))

		if req.Prompt != "" {
			fmt.Fprintf(&b, "### Input Prompt\n\n```json\n%s\n```\n\n", req.Prompt)
		}

		fmt.Fprintf(&b, "### Comparison Summary\n\n")
		fmt.Fprintf(&b, "| Model | Latency | Tokens (In / Out / Total) | Status |\n")
		fmt.Fprintf(&b, "| :--- | :---: | :---: | :---: |\n")
		for _, r := range req.Results {
			statusBadge := "✅ OK"
			if !r.Success {
				statusBadge = fmt.Sprintf("❌ FAIL (%d)", r.StatusCode)
			}
			fmt.Fprintf(&b, "| **%s** | %d ms | %d / %d / %d | %s |\n",
				r.Model, r.LatencyMs, r.PromptTokens, r.CompletionTokens, r.TotalTokens, statusBadge)
		}
		fmt.Fprintf(&b, "\n### Model Outputs\n\n")
		for _, r := range req.Results {
			fmt.Fprintf(&b, "#### %s\n\n", r.Model)
			if r.Success {
				fmt.Fprintf(&b, "```\n%s\n```\n\n", strings.TrimSpace(r.OutputText))
			} else {
				fmt.Fprintf(&b, "> ⚠️ Error: %s\n\n", r.ErrorMsg)
			}
		}
		_, _ = w.Write([]byte(b.String()))
	}
}
