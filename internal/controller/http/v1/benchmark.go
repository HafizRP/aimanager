package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"
)

type BenchmarkResult struct {
	Model      string  `json:"model"`
	LatencyMs  int64   `json:"latency_ms"`
	TTFTMs     int64   `json:"ttft_ms"`
	TokensSec  float64 `json:"tokens_sec"`
	StatusCode int     `json:"status_code"`
	Success    bool    `json:"success"`
	OutputText string  `json:"output_text"`
	ErrorMsg   string  `json:"error_msg,omitempty"`
}

func (h *Handler) BenchmarkPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	models, _ := h.fetchUpstreamModels(ctx)

	h.render(w, r, "benchmark.html", "base.html", map[string]interface{}{
		"ActivePage": "benchmark",
		"Models":     models,
	})
}

func (h *Handler) APIBenchmarkRun(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req struct {
		Models []string `json:"models"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	if len(req.Models) == 0 {
		// Default benchmark models
		req.Models = []string{"main", "ag/gemini-3.8-flash-high", "free-only"}
	}
	if len(req.Models) > 10 {
		req.Models = req.Models[:10]
	}

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
		}
	}

	if userKey == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "No active API key found for benchmarking"})
		return
	}

	var wg sync.WaitGroup
	results := make([]BenchmarkResult, len(req.Models))

	for i, m := range req.Models {
		wg.Add(1)
		go func(idx int, modelName string) {
			defer wg.Done()
			results[idx] = h.benchmarkSingleModel(modelName, userKey)
		}(i, m)
	}

	wg.Wait()

	// Sort results by LatencyMs (fastest first, successful first)
	sort.Slice(results, func(i, j int) bool {
		if results[i].Success != results[j].Success {
			return results[i].Success
		}
		return results[i].LatencyMs < results[j].LatencyMs
	})

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"results": results,
	})
}

func (h *Handler) benchmarkSingleModel(modelName, apiKey string) BenchmarkResult {
	start := time.Now()
	res := BenchmarkResult{
		Model:      modelName,
		StatusCode: 0,
	}

	payload := map[string]interface{}{
		"model": modelName,
		"messages": []map[string]string{
			{"role": "user", "content": "Respond with the single word PONG."},
		},
		"max_tokens":  5,
		"temperature": 0.1,
	}

	b, _ := json.Marshal(payload)
	targetURL := fmt.Sprintf("http://127.0.0.1:%d/v1/chat/completions", h.cfg.Port)

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(context.Background(), "POST", targetURL, bytes.NewReader(b))
	if err != nil {
		res.ErrorMsg = err.Error()
		return res
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	latency := time.Since(start).Milliseconds()
	res.LatencyMs = latency
	res.TTFTMs = latency // Approx for non-streamed prompt

	if err != nil {
		res.ErrorMsg = err.Error()
		return res
	}
	defer resp.Body.Close()
	res.StatusCode = resp.StatusCode

	if resp.StatusCode == http.StatusOK {
		res.Success = true
		var parsed struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
			Usage struct {
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&parsed); err == nil {
			if len(parsed.Choices) > 0 {
				res.OutputText = parsed.Choices[0].Message.Content
			}
			if latency > 0 && parsed.Usage.CompletionTokens > 0 {
				res.TokensSec = float64(parsed.Usage.CompletionTokens) / (float64(latency) / 1000.0)
			}
		}
	} else {
		res.ErrorMsg = "Upstream HTTP Error"
	}

	return res
}
