package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"9router-gateway/internal/config"
	"9router-gateway/internal/models"
	"9router-gateway/internal/repository"
)

type GatewayProxy struct {
	cfg        *config.Config
	repo       repository.Repository
	httpClient *http.Client
}

func NewGatewayProxy(cfg *config.Config, repo repository.Repository) *GatewayProxy {
	return &GatewayProxy{
		cfg:  cfg,
		repo: repo,
		httpClient: &http.Client{
			Timeout: 0, // No global timeout for streaming SSE requests
		},
	}
}

func (p *GatewayProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	clientIP := getClientIP(r)

	// Handle CORS preflight
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, x-api-key")
		w.WriteHeader(http.StatusOK)
		return
	}

	w.Header().Set("Access-Control-Allow-Origin", "*")

	// 1. Authenticate Request
	rawKey := extractAPIKey(r)
	if rawKey == "" {
		p.writeJSONError(w, http.StatusUnauthorized, "API key required. Provide 'Authorization: Bearer <key>' or 'x-api-key: <key>' header.", "invalid_request_error")
		return
	}

	ctx := r.Context()
	key, err := p.repo.GetAPIKeyByKey(ctx, rawKey)
	if err != nil || !key.IsActive {
		p.writeJSONError(w, http.StatusUnauthorized, "Invalid, revoked, or inactive API key", "invalid_request_error")
		return
	}

	user, err := p.repo.GetUserByID(ctx, key.UserID)
	if err != nil || !user.IsActive {
		p.writeJSONError(w, http.StatusForbidden, "User account is suspended or inactive", "permission_denied")
		return
	}

	// 2. Check Token Quota
	if user.TokenQuota > 0 && user.TokensUsed >= user.TokenQuota {
		msg := fmt.Sprintf("Token quota limit reached (%d / %d tokens used). Please contact admin to increase quota.", user.TokensUsed, user.TokenQuota)
		p.writeJSONError(w, http.StatusTooManyRequests, msg, "insufficient_quota")
		return
	}

	// 3. Handle Special Endpoint: GET /v1/models (Model Whitelist Filtering)
	if (r.URL.Path == "/v1/models" || r.URL.Path == "/models") && r.Method == http.MethodGet {
		p.handleGetModels(w, r, user, key)
		return
	}

	// 4. Handle Completions / Upstream Forwarding
	p.handleForwardRequest(w, r, user, key, startTime, clientIP)
}

func (p *GatewayProxy) handleGetModels(w http.ResponseWriter, r *http.Request, user *models.User, key *models.APIKey) {
	upstreamReq, err := http.NewRequestWithContext(r.Context(), http.MethodGet, p.cfg.UpstreamURL+"/v1/models", nil)
	if err != nil {
		p.writeJSONError(w, http.StatusBadGateway, "Failed to create upstream request", "gateway_error")
		return
	}

	upstreamReq.Header.Set("Authorization", "Bearer "+p.cfg.UpstreamAPIKey)
	resp, err := p.httpClient.Do(upstreamReq)
	if err != nil {
		p.writeJSONError(w, http.StatusBadGateway, "Failed to contact 9router upstream: "+err.Error(), "upstream_error")
		return
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		p.writeJSONError(w, http.StatusInternalServerError, "Failed to read upstream response", "gateway_error")
		return
	}

	if resp.StatusCode != http.StatusOK {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(bodyBytes)
		return
	}

	// Parse models list
	var modelsResp struct {
		Object string                   `json:"object"`
		Data   []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(bodyBytes, &modelsResp); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write(bodyBytes)
		return
	}

	// Parse user allowed models
	allowedList := parseAllowedModels(user.AllowedModels)
	if hasWildcard(allowedList) {
		// Return full catalog
		w.Header().Set("Content-Type", "application/json")
		w.Write(bodyBytes)
		return
	}

	// Filter models based on whitelist
	allowedMap := make(map[string]bool)
	for _, m := range allowedList {
		allowedMap[strings.TrimSpace(m)] = true
	}

	filteredData := []map[string]interface{}{}
	for _, m := range modelsResp.Data {
		if id, ok := m["id"].(string); ok && allowedMap[id] {
			filteredData = append(filteredData, m)
		}
	}

	modelsResp.Data = filteredData
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(modelsResp)
}

func (p *GatewayProxy) handleForwardRequest(w http.ResponseWriter, r *http.Request, user *models.User, key *models.APIKey, startTime time.Time, clientIP string) {
	// Read body for inspection
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		p.writeJSONError(w, http.StatusBadRequest, "Failed to read request body", "invalid_request_error")
		return
	}
	_ = r.Body.Close()

	var reqBodyMap map[string]interface{}
	_ = json.Unmarshal(bodyBytes, &reqBodyMap)

	requestedModel := ""
	isStreamRequested := false
	if reqBodyMap != nil {
		if m, ok := reqBodyMap["model"].(string); ok {
			requestedModel = m
		}
		if s, ok := reqBodyMap["stream"].(bool); ok {
			isStreamRequested = s
		}
	}

	// Model Whitelist Check (for requests specifying a model)
	if requestedModel != "" {
		allowedList := parseAllowedModels(user.AllowedModels)
		if !hasWildcard(allowedList) && !isModelAllowed(requestedModel, allowedList) {
			duration := time.Since(startTime).Milliseconds()
			errMsg := fmt.Sprintf("Model '%s' is not allowed for your API key. Allowed models: %v", requestedModel, allowedList)
			p.writeJSONError(w, http.StatusForbidden, errMsg, "permission_denied")

			// Log unauthorized attempt
			_ = p.repo.CreateRequestLog(r.Context(), &models.RequestLog{
				UserID:       user.ID,
				APIKeyID:     key.ID,
				Path:         r.URL.Path,
				Method:       r.Method,
				Model:        requestedModel,
				IsStream:     isStreamRequested,
				StatusCode:   http.StatusForbidden,
				DurationMs:   duration,
				ClientIP:     clientIP,
				ErrorMessage: errMsg,
			})
			return
		}
	}

	// Prepare outbound upstream request
	upstreamURL := p.cfg.UpstreamURL + r.URL.RequestURI()
	upstreamReq, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		p.writeJSONError(w, http.StatusInternalServerError, "Failed to build upstream request", "gateway_error")
		return
	}

	// Copy headers
	for k, vv := range r.Header {
		for _, v := range vv {
			upstreamReq.Header.Add(k, v)
		}
	}
	// Inject Upstream Key
	upstreamReq.Header.Set("Authorization", "Bearer "+p.cfg.UpstreamAPIKey)
	upstreamReq.Header.Set("X-Forwarded-For", clientIP)

	resp, err := p.httpClient.Do(upstreamReq)
	if err != nil {
		duration := time.Since(startTime).Milliseconds()
		errMsg := "Upstream 9router connection error: " + err.Error()
		p.writeJSONError(w, http.StatusBadGateway, errMsg, "upstream_error")

		_ = p.repo.CreateRequestLog(r.Context(), &models.RequestLog{
			UserID:       user.ID,
			APIKeyID:     key.ID,
			Path:         r.URL.Path,
			Method:       r.Method,
			Model:        requestedModel,
			IsStream:     isStreamRequested,
			StatusCode:   http.StatusBadGateway,
			DurationMs:   duration,
			ClientIP:     clientIP,
			ErrorMessage: errMsg,
		})
		return
	}
	defer resp.Body.Close()

	contentType := resp.Header.Get("Content-Type")
	isSSE := strings.Contains(contentType, "text/event-stream") || isStreamRequested

	if isSSE && resp.StatusCode == http.StatusOK {
		p.handleStreamingResponse(w, r, resp, user, key, requestedModel, startTime, clientIP)
		return
	}

	// Non-streaming response
	p.handleNonStreamingResponse(w, r, resp, user, key, requestedModel, startTime, clientIP, len(bodyBytes))
}

func (p *GatewayProxy) handleStreamingResponse(w http.ResponseWriter, r *http.Request, resp *http.Response, user *models.User, key *models.APIKey, model string, startTime time.Time, clientIP string) {
	flusher, isFlusher := w.(http.Flusher)
	if !isFlusher {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	// Copy relevant headers
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(resp.StatusCode)
	flusher.Flush()

	scanner := bufio.NewScanner(resp.Body)
	// Allow large tokens / lines up to 1MB
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var promptTokens, completionTokens, totalTokens int
	var completionChars int

	for scanner.Scan() {
		line := scanner.Bytes()
		// Forward chunk immediately to client
		_, _ = w.Write(line)
		_, _ = w.Write([]byte("\n"))
		flusher.Flush()

		lineStr := string(line)
		if strings.HasPrefix(lineStr, "data: ") {
			dataPayload := strings.TrimPrefix(lineStr, "data: ")
			if strings.TrimSpace(dataPayload) == "[DONE]" {
				continue
			}

			var chunk struct {
				Usage *struct {
					PromptTokens     int `json:"prompt_tokens"`
					CompletionTokens int `json:"completion_tokens"`
					TotalTokens      int `json:"total_tokens"`
				} `json:"usage"`
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				} `json:"choices"`
			}

			if err := json.Unmarshal([]byte(dataPayload), &chunk); err == nil {
				if chunk.Usage != nil && chunk.Usage.TotalTokens > 0 {
					promptTokens = chunk.Usage.PromptTokens
					completionTokens = chunk.Usage.CompletionTokens
					totalTokens = chunk.Usage.TotalTokens
				}
				for _, c := range chunk.Choices {
					completionChars += len(c.Delta.Content)
				}
			}
		}
	}

	duration := time.Since(startTime).Milliseconds()

	// If upstream didn't send usage in SSE, estimate tokens
	if totalTokens == 0 {
		promptTokens = 50 // default base prompt estimate
		completionTokens = completionChars/4 + 1
		totalTokens = promptTokens + completionTokens
	}

	// Deduct tokens and log
	_ = p.repo.DeductTokens(context.Background(), user.ID, totalTokens)
	_ = p.repo.UpdateKeyLastUsed(context.Background(), key.ID)
	_ = p.repo.CreateRequestLog(context.Background(), &models.RequestLog{
		UserID:           user.ID,
		APIKeyID:         key.ID,
		Path:             r.URL.Path,
		Method:           r.Method,
		Model:            model,
		IsStream:         true,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      totalTokens,
		StatusCode:       resp.StatusCode,
		DurationMs:       duration,
		ClientIP:         clientIP,
	})
}

func (p *GatewayProxy) handleNonStreamingResponse(w http.ResponseWriter, r *http.Request, resp *http.Response, user *models.User, key *models.APIKey, model string, startTime time.Time, clientIP string, reqBodyLen int) {
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		p.writeJSONError(w, http.StatusInternalServerError, "Failed to read upstream response", "gateway_error")
		return
	}

	duration := time.Since(startTime).Milliseconds()
	var promptTokens, completionTokens, totalTokens int

	if resp.StatusCode == http.StatusOK {
		var respMap struct {
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				TotalTokens      int `json:"total_tokens"`
				InputTokens      int `json:"input_tokens"`  // Anthropic format
				OutputTokens     int `json:"output_tokens"` // Anthropic format
			} `json:"usage"`
		}
		if err := json.Unmarshal(respBytes, &respMap); err == nil && respMap.Usage != nil {
			if respMap.Usage.TotalTokens > 0 {
				promptTokens = respMap.Usage.PromptTokens
				completionTokens = respMap.Usage.CompletionTokens
				totalTokens = respMap.Usage.TotalTokens
			} else if respMap.Usage.InputTokens > 0 || respMap.Usage.OutputTokens > 0 {
				promptTokens = respMap.Usage.InputTokens
				completionTokens = respMap.Usage.OutputTokens
				totalTokens = promptTokens + completionTokens
			}
		}

		if totalTokens == 0 {
			promptTokens = reqBodyLen / 4
			completionTokens = len(respBytes) / 4
			totalTokens = promptTokens + completionTokens
		}

		// Deduct tokens
		_ = p.repo.DeductTokens(context.Background(), user.ID, totalTokens)
		_ = p.repo.UpdateKeyLastUsed(context.Background(), key.ID)
	}

	// Copy headers
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(respBytes)

	// Log request
	errMsg := ""
	if resp.StatusCode >= 400 {
		errMsg = fmt.Sprintf("Upstream returned HTTP %d", resp.StatusCode)
	}
	_ = p.repo.CreateRequestLog(context.Background(), &models.RequestLog{
		UserID:           user.ID,
		APIKeyID:         key.ID,
		Path:             r.URL.Path,
		Method:           r.Method,
		Model:            model,
		IsStream:         false,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      totalTokens,
		StatusCode:       resp.StatusCode,
		DurationMs:       duration,
		ClientIP:         clientIP,
		ErrorMessage:     errMsg,
	})
}

func (p *GatewayProxy) writeJSONError(w http.ResponseWriter, statusCode int, message, errType string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{
			"message": message,
			"type":    errType,
			"code":    statusCode,
		},
	})
}

func extractAPIKey(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			return strings.TrimSpace(parts[1])
		}
		return strings.TrimSpace(authHeader)
	}
	if xKey := r.Header.Get("x-api-key"); xKey != "" {
		return strings.TrimSpace(xKey)
	}
	return ""
}

func getClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	if xrip := r.Header.Get("X-Real-IP"); xrip != "" {
		return strings.TrimSpace(xrip)
	}
	parts := strings.Split(r.RemoteAddr, ":")
	if len(parts) > 0 {
		return parts[0]
	}
	return r.RemoteAddr
}

func parseAllowedModels(raw string) []string {
	var list []string
	if strings.TrimSpace(raw) == "" {
		return []string{"*"}
	}
	if err := json.Unmarshal([]byte(raw), &list); err == nil {
		return list
	}
	// Fallback comma-separated
	parts := strings.Split(raw, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			list = append(list, p)
		}
	}
	if len(list) == 0 {
		return []string{"*"}
	}
	return list
}

func hasWildcard(list []string) bool {
	for _, m := range list {
		if strings.TrimSpace(m) == "*" {
			return true
		}
	}
	return false
}

func isModelAllowed(requested string, allowedList []string) bool {
	for _, a := range allowedList {
		if a == "*" || strings.EqualFold(a, requested) {
			return true
		}
	}
	return false
}
