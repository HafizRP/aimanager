package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"9router-gateway/internal/config"
	"9router-gateway/internal/entity"
	"9router-gateway/internal/repository"
)

type GatewayProxy struct {
	cfg         *config.Config
	repo        repository.Repository
	httpClient  *http.Client
	rateLimiter *RateLimiter
	cache       *ResponseCache
}

func NewGatewayProxy(cfg *config.Config, repo repository.Repository) *GatewayProxy {
	return &GatewayProxy{
		cfg:  cfg,
		repo: repo,
		httpClient: &http.Client{
			Timeout: 0, // No global timeout for streaming SSE requests
		},
		rateLimiter: NewRateLimiter(),
		cache:       NewResponseCache(),
	}
}

func (p *GatewayProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	clientIP := GetClientIP(r)

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
		p.writeJSONError(w, http.StatusUnauthorized, "API key required. Provide 'Authorization: Bearer ***' or 'x-api-key: *** header.", "invalid_request_error")
		return
	}

	ctx := r.Context()
	key, err := p.repo.GetAPIKeyByKey(ctx, rawKey)
	if err != nil || !key.IsActive {
		p.writeJSONError(w, http.StatusUnauthorized, "Invalid, revoked, or inactive API key", "invalid_request_error")
		return
	}

	// 1a. Key Expiry Enforcement
	if key.IsExpired() {
		p.writeJSONError(w, http.StatusUnauthorized, "API key has expired. Please contact administrator.", "invalid_request_error")
		return
	}

	user, err := p.repo.GetUserByID(ctx, key.UserID)
	if err != nil || !user.IsActive {
		p.writeJSONError(w, http.StatusForbidden, "User account is suspended or inactive", "permission_denied")
		return
	}

	// 1b. Rate Limiting Check (Key-level & User-level)
	if key.RateLimitRPM > 0 {
		allowed, msg := p.rateLimiter.Allow("key:"+key.ID, key.RateLimitRPM, 0, 0)
		if !allowed {
			w.Header().Set("Retry-After", "60")
			p.writeJSONError(w, http.StatusTooManyRequests, msg, "rate_limit_exceeded")
			return
		}
	}
	if user.RateLimitRPM > 0 || user.RateLimitTPM > 0 {
		allowed, msg := p.rateLimiter.Allow("user:"+user.ID, user.RateLimitRPM, user.RateLimitTPM, 200)
		if !allowed {
			w.Header().Set("Retry-After", "60")
			p.writeJSONError(w, http.StatusTooManyRequests, msg, "rate_limit_exceeded")
			return
		}
	}

	// 2. Check Token Quota (Admins are exempt from quota lockout)
	if !user.IsAdmin() && user.TokenQuota > 0 && user.TokensUsed >= user.TokenQuota {
		msg := fmt.Sprintf("Token quota limit reached (%d / %d tokens used). Please purchase tokens or contact admin.", user.TokensUsed, user.TokenQuota)
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

func (p *GatewayProxy) handleGetModels(w http.ResponseWriter, r *http.Request, user *entity.User, key *entity.APIKey) {
	upstreamReq, err := http.NewRequestWithContext(r.Context(), http.MethodGet, p.cfg.GetUpstreamURL()+"/v1/models", nil)
	if err != nil {
		p.writeJSONError(w, http.StatusBadGateway, "Failed to create upstream request", "gateway_error")
		return
	}

	authKey := key.Key
	if authKey == "" {
		authKey = p.cfg.GetUpstreamAPIKey()
	}
	upstreamReq.Header.Set("Authorization", "Bearer "+authKey)
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
	allowedList := entity.ParseAllowedModels(user.AllowedModels)
	if entity.HasWildcard(allowedList) {
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

func (p *GatewayProxy) handleForwardRequest(w http.ResponseWriter, r *http.Request, user *entity.User, key *entity.APIKey, startTime time.Time, clientIP string) {
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
		if !user.HasModelAccess(requestedModel) {
			allowedList := user.GetAllowedModels()
			duration := time.Since(startTime).Milliseconds()
			errMsg := fmt.Sprintf("Model '%s' is not allowed for your API key. Allowed models: %v", requestedModel, allowedList)
			p.writeJSONError(w, http.StatusForbidden, errMsg, "permission_denied")

			// Log unauthorized attempt
			_ = p.repo.CreateRequestLog(r.Context(), &entity.RequestLog{
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

		// Key-level Model Whitelist Check
		if key.AllowedModels != "" && key.AllowedModels != "[]" && key.AllowedModels != "[\"*\"]" {
			if !key.HasModelAccess(requestedModel) {
				keyAllowed := key.GetAllowedModels()
				duration := time.Since(startTime).Milliseconds()
				errMsg := fmt.Sprintf("Model '%s' is not allowed for this specific API key. Allowed key models: %v", requestedModel, keyAllowed)
				p.writeJSONError(w, http.StatusForbidden, errMsg, "permission_denied")

				_ = p.repo.CreateRequestLog(r.Context(), &entity.RequestLog{
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
	}

	// Exact Response Cache Check (Non-streaming completions)
	var cacheKey string
	if !isStreamRequested && reqBodyMap != nil && r.Method == http.MethodPost {
		temp := 0.7
		if t, ok := reqBodyMap["temperature"].(float64); ok {
			temp = t
		}
		cacheKey = p.cache.GenerateKey(requestedModel, reqBodyMap["messages"], temp)
		if cached, found := p.cache.Get(cacheKey); found {
			// CACHE HIT
			w.Header().Set("Content-Type", cached.ContentType)
			w.Header().Set("X-Cache", "HIT")
			w.WriteHeader(cached.StatusCode)
			_, _ = w.Write(cached.Body)

			_ = p.repo.UpdateKeyLastUsed(r.Context(), key.ID)
			_ = p.repo.CreateRequestLog(r.Context(), &entity.RequestLog{
				UserID:     user.ID,
				APIKeyID:   key.ID,
				Path:       r.URL.Path,
				Method:     r.Method,
				Model:      requestedModel,
				IsStream:   false,
				StatusCode: cached.StatusCode,
				DurationMs: 1,
				ClientIP:   clientIP,
			})
			return
		}
		// Cacheable request but no entry yet → miss is already counted inside Get()
	} else {
		// Streaming or non-cacheable → count as miss (no cache entry possible)
		p.cache.RecordMiss()
	}

	// Prepare outbound upstream request
	upstreamURL := p.cfg.GetUpstreamURL() + r.URL.RequestURI()
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
	authKey := key.Key
	if authKey == "" {
		authKey = p.cfg.GetUpstreamAPIKey()
	}
	upstreamReq.Header.Set("Authorization", "Bearer "+authKey)
	upstreamReq.Header.Set("X-Forwarded-For", clientIP)

	resp, err := p.httpClient.Do(upstreamReq)
	if err != nil {
		duration := time.Since(startTime).Milliseconds()
		errMsg := "Upstream 9router connection error: " + err.Error()
		p.writeJSONError(w, http.StatusBadGateway, errMsg, "upstream_error")

		_ = p.repo.CreateRequestLog(r.Context(), &entity.RequestLog{
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

	// Record measured upstream round-trip latency for cache analytics (miss path only)
	if requestedModel != "" {
		p.cache.RecordUpstreamLatency(requestedModel, time.Since(startTime).Milliseconds())
	}

	contentType := resp.Header.Get("Content-Type")
	isSSE := strings.Contains(contentType, "text/event-stream") || isStreamRequested

	if isSSE && resp.StatusCode == http.StatusOK {
		p.handleStreamingResponse(w, r, resp, user, key, requestedModel, startTime, clientIP)
		return
	}

	// Non-streaming response
	p.handleNonStreamingResponse(w, r, resp, user, key, requestedModel, startTime, clientIP, len(bodyBytes), cacheKey)
}

func (p *GatewayProxy) handleStreamingResponse(w http.ResponseWriter, r *http.Request, resp *http.Response, user *entity.User, key *entity.APIKey, model string, startTime time.Time, clientIP string) {
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
	_ = p.repo.CreateRequestLog(context.Background(), &entity.RequestLog{
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

func (p *GatewayProxy) handleNonStreamingResponse(w http.ResponseWriter, r *http.Request, resp *http.Response, user *entity.User, key *entity.APIKey, model string, startTime time.Time, clientIP string, reqBodyLen int, cacheKey string) {
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

		// Save response to Exact Match Cache
		if cacheKey != "" && len(respBytes) > 0 {
			p.cache.Set(cacheKey, respBytes, resp.StatusCode, resp.Header.Get("Content-Type"), totalTokens, model, 15*time.Minute)
		}
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
	_ = p.repo.CreateRequestLog(context.Background(), &entity.RequestLog{
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

// GetClientIP extracts client IP address from various standard HTTP headers.
func GetClientIP(r *http.Request) string {
	if cfIP := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); cfIP != "" {
		return cfIP
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		ip := strings.TrimSpace(parts[0])
		if ip != "" {
			return ip
		}
	}
	if xrip := strings.TrimSpace(r.Header.Get("X-Real-IP")); xrip != "" {
		return xrip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}
