package v1

import (
	"bufio"
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

	"9router-gateway/internal/entity"
)

// BenchmarkResult holds the outcome of a single benchmark run.
type BenchmarkResult struct {
	Model      string  `json:"model"`
	LatencyMs  int64   `json:"latency_ms"`
	TTFTMs     int64   `json:"ttft_ms"`
	TokensSec  float64 `json:"tokens_sec"`
	StatusCode int     `json:"status_code"`
	Success    bool    `json:"success"`
	OutputText string  `json:"output_text"`
	ErrorMsg   string  `json:"error_msg,omitempty"`
	IsStream   bool    `json:"is_stream"`
}

// BenchmarkStats holds aggregated performance metrics from a benchmark run.
type BenchmarkStats struct {
	FastestModel     string  `json:"fastest_model"`
	FastestLatencyMs int64   `json:"fastest_latency_ms"`
	FastestTTFTMs    int64   `json:"fastest_ttft_ms"`
	AvgLatencyMs     int64   `json:"avg_latency_ms"`
	PeakTokensSec    float64 `json:"peak_tokens_sec"`
	SuccessRatePct   float64 `json:"success_rate_pct"`
	TotalTested      int     `json:"total_tested"`
}

type benchmarkOptions struct {
	modelName   string
	apiKey      string
	prompt      string
	maxTokens   int
	temperature float64
	stream      bool
}

// BenchmarkPage renders the speed benchmark page.
func (h *Handler) BenchmarkPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)

	// Fetch models & combos
	allModels, _ := h.fetchUpstreamModels(ctx)
	combos, _ := h.coreClient.GetCombos(ctx)

	// Filter models by whitelist if standard user
	var availableModels []UpstreamModelItem
	if currentUser != nil && !currentUser.IsAdmin() {
		allowed := entity.ParseAllowedModels(currentUser.AllowedModels)
		isWildcard := len(allowed) == 1 && allowed[0] == "*"
		for _, m := range allModels {
			match := isWildcard
			if !match {
				for _, a := range allowed {
					if a == m.ID {
						match = true
						break
					}
				}
			}
			if match {
				availableModels = append(availableModels, m)
			}
		}
	} else {
		availableModels = allModels
	}

	h.render(w, r, "benchmark.html", "base.html", map[string]interface{}{
		"ActivePage": "benchmark",
		"Models":     availableModels,
		"Combos":     combos,
	})
}

// APIBenchmarkRun runs a latency benchmark against the upstream providers.
func (h *Handler) APIBenchmarkRun(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req struct {
		Models      []string `json:"models"`
		Prompt      string   `json:"prompt"`
		MaxTokens   int      `json:"max_tokens"`
		Temperature float64  `json:"temperature"`
		Stream      bool     `json:"stream"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	if len(req.Models) == 0 {
		// Default benchmark models
		req.Models = []string{"main", "free-only"}
	}
	if len(req.Models) > 12 {
		req.Models = req.Models[:12]
	}

	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		prompt = "Respond with the single word PONG."
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 || maxTokens > 250 {
		maxTokens = 10
	}
	temp := req.Temperature
	if temp < 0 || temp > 2.0 {
		temp = 0.1
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

	// Filter models by user access if non-admin
	var modelsToRun []string
	if currentUser != nil && !currentUser.IsAdmin() {
		allowed := entity.ParseAllowedModels(currentUser.AllowedModels)
		for _, m := range req.Models {
			if entity.IsModelAllowed(m, allowed) {
				modelsToRun = append(modelsToRun, m)
			}
		}
	} else {
		modelsToRun = req.Models
	}

	if len(modelsToRun) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "None of the selected models are accessible"})
		return
	}

	var wg sync.WaitGroup
	results := make([]BenchmarkResult, len(modelsToRun))

	for i, m := range modelsToRun {
		wg.Add(1)
		go func(idx int, modelName string) {
			defer wg.Done()
			results[idx] = h.benchmarkSingleModel(benchmarkOptions{
				modelName:   modelName,
				apiKey:      userKey,
				prompt:      prompt,
				maxTokens:   maxTokens,
				temperature: temp,
				stream:      req.Stream,
			})
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

	stats := calculateBenchmarkStats(results)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"results": results,
		"stats":   stats,
	})
}

// APIBenchmarkExport exports benchmark results to CSV or JSON file.
func (h *Handler) APIBenchmarkExport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Format  string            `json:"format"`
		Results []BenchmarkResult `json:"results"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	format := strings.ToLower(req.Format)
	if format != "json" {
		format = "csv"
	}

	timestamp := time.Now().Format("20060102_150405")

	if format == "json" {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=benchmark_results_%s.json", timestamp))
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"exported_at": time.Now().UTC().Format(time.RFC3339),
			"total_count": len(req.Results),
			"results":     req.Results,
		})
		return
	}

	// CSV format
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=benchmark_results_%s.csv", timestamp))

	writer := csv.NewWriter(w)
	defer writer.Flush()

	_ = writer.Write([]string{
		"Rank", "Model", "Latency_MS", "TTFT_MS", "Tokens_Per_Sec", "Status_Code", "Success", "Streaming", "Output_Sample", "Error",
	})

	for i, res := range req.Results {
		statusStr := "OK"
		if !res.Success {
			statusStr = "Failed"
		}
		_ = writer.Write([]string{
			strconv.Itoa(i + 1),
			res.Model,
			strconv.FormatInt(res.LatencyMs, 10),
			strconv.FormatInt(res.TTFTMs, 10),
			fmt.Sprintf("%.2f", res.TokensSec),
			strconv.Itoa(res.StatusCode),
			statusStr,
			strconv.FormatBool(res.IsStream),
			res.OutputText,
			res.ErrorMsg,
		})
	}
}

func calculateBenchmarkStats(results []BenchmarkResult) BenchmarkStats {
	stats := BenchmarkStats{
		TotalTested: len(results),
	}
	if len(results) == 0 {
		return stats
	}

	var successCount int
	var totalLatency int64
	var minLatency int64 = -1
	var minTTFT int64 = -1
	var peakTokensSec float64

	for _, r := range results {
		if r.Success {
			successCount++
			totalLatency += r.LatencyMs

			if minLatency == -1 || r.LatencyMs < minLatency {
				minLatency = r.LatencyMs
				stats.FastestModel = r.Model
				stats.FastestLatencyMs = r.LatencyMs
			}

			if r.TTFTMs > 0 && (minTTFT == -1 || r.TTFTMs < minTTFT) {
				minTTFT = r.TTFTMs
				stats.FastestTTFTMs = r.TTFTMs
			}

			if r.TokensSec > peakTokensSec {
				peakTokensSec = r.TokensSec
			}
		}
	}

	if stats.FastestTTFTMs == 0 && stats.FastestLatencyMs > 0 {
		stats.FastestTTFTMs = stats.FastestLatencyMs
	}

	stats.SuccessRatePct = float64(successCount) / float64(len(results)) * 100.0
	if successCount > 0 {
		stats.AvgLatencyMs = totalLatency / int64(successCount)
	}
	stats.PeakTokensSec = peakTokensSec

	return stats
}

func (h *Handler) benchmarkSingleModel(opt benchmarkOptions) BenchmarkResult {
	start := time.Now()
	res := BenchmarkResult{
		Model:      opt.modelName,
		StatusCode: 0,
		IsStream:   opt.stream,
	}

	payload := map[string]interface{}{
		"model": opt.modelName,
		"messages": []map[string]string{
			{"role": "user", "content": opt.prompt},
		},
		"max_tokens":  opt.maxTokens,
		"temperature": opt.temperature,
	}
	if opt.stream {
		payload["stream"] = true
	}

	b, _ := json.Marshal(payload)
	targetURL := fmt.Sprintf("http://127.0.0.1:%d/v1/chat/completions", h.cfg.Port)

	client := &http.Client{Timeout: 25 * time.Second}
	req, err := http.NewRequestWithContext(context.Background(), "POST", targetURL, bytes.NewReader(b))
	if err != nil {
		res.ErrorMsg = err.Error()
		return res
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+opt.apiKey)
	if opt.stream {
		req.Header.Set("Accept", "text/event-stream")
	}

	resp, err := client.Do(req)
	if err != nil {
		res.LatencyMs = time.Since(start).Milliseconds()
		res.ErrorMsg = err.Error()
		return res
	}
	defer resp.Body.Close()

	res.StatusCode = resp.StatusCode

	if resp.StatusCode != http.StatusOK {
		res.LatencyMs = time.Since(start).Milliseconds()
		var errObj struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
			Message string `json:"message"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&errObj); err == nil {
			if errObj.Error.Message != "" {
				res.ErrorMsg = errObj.Error.Message
			} else if errObj.Message != "" {
				res.ErrorMsg = errObj.Message
			} else {
				res.ErrorMsg = fmt.Sprintf("HTTP %d error", resp.StatusCode)
			}
		} else {
			res.ErrorMsg = fmt.Sprintf("HTTP %d error", resp.StatusCode)
		}
		return res
	}

	// Successful response handling
	if opt.stream {
		var output strings.Builder
		var firstTokenRecorded bool
		var chunkCount int

		scanner := bufio.NewScanner(resp.Body)
		buf := make([]byte, 64*1024)
		scanner.Buffer(buf, 256*1024)

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "[DONE]" {
				break
			}

			var chunk struct {
				Choices []struct {
					Delta struct {
						Content          string `json:"content"`
						ReasoningContent string `json:"reasoning_content"`
					} `json:"delta"`
				} `json:"choices"`
			}
			if err := json.Unmarshal([]byte(data), &chunk); err == nil && len(chunk.Choices) > 0 {
				deltaText := chunk.Choices[0].Delta.Content
				if deltaText == "" {
					deltaText = chunk.Choices[0].Delta.ReasoningContent
				}
				if deltaText != "" {
					if !firstTokenRecorded {
						res.TTFTMs = time.Since(start).Milliseconds()
						firstTokenRecorded = true
					}
					output.WriteString(deltaText)
					chunkCount++
				}
			}
		}

		res.LatencyMs = time.Since(start).Milliseconds()
		if !firstTokenRecorded {
			res.TTFTMs = res.LatencyMs
		}
		res.OutputText = strings.TrimSpace(output.String())
		res.Success = true

		if chunkCount > 0 && res.LatencyMs > 0 {
			res.TokensSec = float64(chunkCount) / (float64(res.LatencyMs) / 1000.0)
		}
	} else {
		res.LatencyMs = time.Since(start).Milliseconds()
		res.TTFTMs = res.LatencyMs // Non-stream approximation

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
			res.Success = true
			if len(parsed.Choices) > 0 {
				res.OutputText = strings.TrimSpace(parsed.Choices[0].Message.Content)
			}
			if res.LatencyMs > 0 && parsed.Usage.CompletionTokens > 0 {
				res.TokensSec = float64(parsed.Usage.CompletionTokens) / (float64(res.LatencyMs) / 1000.0)
			} else if res.LatencyMs > 0 && len(res.OutputText) > 0 {
				// Fallback approximation: 1 token ~ 4 chars
				estTokens := len(res.OutputText) / 4
				if estTokens < 1 {
					estTokens = 1
				}
				res.TokensSec = float64(estTokens) / (float64(res.LatencyMs) / 1000.0)
			}
		} else {
			res.Success = true
		}
	}

	return res
}
