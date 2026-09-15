// Package proxy implements the OpenAI/Anthropic-compatible reverse proxy gateway.
package proxy

import (
	"context"
	"fmt"
	"strings"

	"9router-gateway/internal/entity"
	"9router-gateway/internal/upstream"
)

// RoutingContext carries the request details needed by a RoutingStrategy to select a model.
type RoutingContext struct {
	RequestedModel string
	User           *entity.User
	Key            *entity.APIKey
	Combos         []upstream.Combo
	Breakers       *CircuitBreakerRegistry
	Health         *UpstreamHealth
	Cache          *ResponseCache
}

// RoutingDecision represents the chosen target model and strategy metadata.
type RoutingDecision struct {
	SelectedModel string   `json:"selected_model"`
	Strategy      string   `json:"strategy"`
	FallbackChain []string `json:"fallback_chain,omitempty"`
	Reason        string   `json:"reason"`
}

// RoutingStrategy defines the pluggable algorithm interface for selecting upstream models/providers.
type RoutingStrategy interface {
	Name() string
	SelectModel(ctx context.Context, rc *RoutingContext) (*RoutingDecision, error)
}

// DirectRoutingStrategy passes the requested model through directly if its circuit breaker is healthy.
type DirectRoutingStrategy struct{}

func (s *DirectRoutingStrategy) Name() string {
	return "direct"
}

func (s *DirectRoutingStrategy) SelectModel(ctx context.Context, rc *RoutingContext) (*RoutingDecision, error) {
	model := strings.TrimSpace(rc.RequestedModel)
	if model == "" {
		return nil, fmt.Errorf("no model requested")
	}

	if rc.Breakers != nil {
		if cb, exists := rc.Breakers.Get(model); exists && !cb.Allow() {
			return nil, fmt.Errorf("circuit breaker for model %s is open", model)
		}
	}

	return &RoutingDecision{
		SelectedModel: model,
		Strategy:      s.Name(),
		Reason:        "direct passthrough of requested model",
	}, nil
}

// FailoverComboStrategy walks a combo's fallback chain, skipping any model whose circuit breaker is open.
type FailoverComboStrategy struct{}

func (s *FailoverComboStrategy) Name() string {
	return "failover_combo"
}

func (s *FailoverComboStrategy) SelectModel(ctx context.Context, rc *RoutingContext) (*RoutingDecision, error) {
	requested := strings.TrimSpace(rc.RequestedModel)
	var matchingCombo *upstream.Combo
	for _, c := range rc.Combos {
		if strings.EqualFold(c.Name, requested) || strings.EqualFold(c.ID, requested) {
			matchingCombo = &c
			break
		}
	}

	if matchingCombo == nil || len(matchingCombo.Models) == 0 {
		return nil, fmt.Errorf("combo not found: %s", requested)
	}

	for i, m := range matchingCombo.Models {
		if rc.Breakers != nil {
			if cb, exists := rc.Breakers.Get(m); exists && !cb.Allow() {
				// Circuit breaker is open for this model, skip to next in fallback chain
				continue
			}
		}

		var fallbacks []string
		if i+1 < len(matchingCombo.Models) {
			fallbacks = matchingCombo.Models[i+1:]
		}

		return &RoutingDecision{
			SelectedModel: m,
			Strategy:      s.Name(),
			FallbackChain: fallbacks,
			Reason:        fmt.Sprintf("selected healthy model #%d from combo %s", i+1, matchingCombo.Name),
		}, nil
	}

	return nil, fmt.Errorf("all models in combo %s are circuit-broken or unavailable", matchingCombo.Name)
}

// CostOptimizedStrategy prioritizes free and high-quota models.
type CostOptimizedStrategy struct{}

func (s *CostOptimizedStrategy) Name() string {
	return "cost_optimized"
}

func (s *CostOptimizedStrategy) SelectModel(ctx context.Context, rc *RoutingContext) (*RoutingDecision, error) {
	// Preferred free-tier prefixes/models
	freeCandidates := []string{
		"oc/muse-spark-1.3-contributor-free",
		"ag/gemini-3.8-flash",
		"ag/gemini-3.8-flash-high",
		"ag/gemini-3.7-flash",
	}

	for _, m := range freeCandidates {
		// Check user whitelist access
		if rc.User != nil && !rc.User.HasModelAccess(m) {
			continue
		}
		if rc.Key != nil && rc.Key.AllowedModels != "" && !rc.Key.HasModelAccess(m) {
			continue
		}
		// Check circuit breaker
		if rc.Breakers != nil {
			if cb, exists := rc.Breakers.Get(m); exists && !cb.Allow() {
				continue
			}
		}

		return &RoutingDecision{
			SelectedModel: m,
			Strategy:      s.Name(),
			Reason:        "selected cost-free/quota-optimized model tier",
		}, nil
	}

	// Fall back to direct if no free candidate is accessible
	direct := &DirectRoutingStrategy{}
	return direct.SelectModel(ctx, rc)
}

// LatencyOptimizedStrategy picks the candidate with the lowest average latency from cache statistics.
type LatencyOptimizedStrategy struct{}

func (s *LatencyOptimizedStrategy) Name() string {
	return "latency_optimized"
}

func (s *LatencyOptimizedStrategy) SelectModel(ctx context.Context, rc *RoutingContext) (*RoutingDecision, error) {
	// If a combo is provided, select lowest latency among combo models
	requested := strings.TrimSpace(rc.RequestedModel)
	var candidates []string

	for _, c := range rc.Combos {
		if strings.EqualFold(c.Name, requested) || strings.EqualFold(c.ID, requested) {
			candidates = c.Models
			break
		}
	}

	if len(candidates) == 0 {
		candidates = []string{requested}
	}

	var bestModel string
	bestLatency := int64(999999)

	for _, m := range candidates {
		if rc.Breakers != nil {
			if cb, exists := rc.Breakers.Get(m); exists && !cb.Allow() {
				continue
			}
		}

		latency := int64(500) // default assumed latency
		if rc.Cache != nil {
			rc.Cache.mu.RLock()
			if n := rc.Cache.latencyN[m]; n > 0 {
				latency = rc.Cache.avgLatency[m] / n
			}
			rc.Cache.mu.RUnlock()
		}

		if latency < bestLatency {
			bestLatency = latency
			bestModel = m
		}
	}

	if bestModel == "" {
		direct := &DirectRoutingStrategy{}
		return direct.SelectModel(ctx, rc)
	}

	return &RoutingDecision{
		SelectedModel: bestModel,
		Strategy:      s.Name(),
		Reason:        fmt.Sprintf("selected lowest measured latency (~%dms)", bestLatency),
	}, nil
}

// ModelRouter manages and coordinates pluggable routing strategies.
type ModelRouter struct {
	strategies map[string]RoutingStrategy
}

// NewModelRouter creates a ModelRouter with standard built-in strategies.
func NewModelRouter() *ModelRouter {
	mr := &ModelRouter{
		strategies: make(map[string]RoutingStrategy),
	}
	mr.Register(&DirectRoutingStrategy{})
	mr.Register(&FailoverComboStrategy{})
	mr.Register(&CostOptimizedStrategy{})
	mr.Register(&LatencyOptimizedStrategy{})
	return mr
}

// Register adds or updates a routing strategy.
func (mr *ModelRouter) Register(strategy RoutingStrategy) {
	mr.strategies[strategy.Name()] = strategy
}

// Route determines the target model by picking the most suitable strategy.
func (mr *ModelRouter) Route(ctx context.Context, rc *RoutingContext) (*RoutingDecision, error) {
	model := strings.ToLower(strings.TrimSpace(rc.RequestedModel))

	// 1. Check if model explicitly targets cost optimization
	if model == "free-only" || model == "cost-optimized" || model == "cheapest" {
		if strat, ok := mr.strategies["cost_optimized"]; ok {
			return strat.SelectModel(ctx, rc)
		}
	}

	// 2. Check if model explicitly targets latency optimization
	if model == "fastest" || model == "lowest-latency" {
		if strat, ok := mr.strategies["latency_optimized"]; ok {
			return strat.SelectModel(ctx, rc)
		}
	}

	// 3. Check if model matches any combo name
	for _, c := range rc.Combos {
		if strings.EqualFold(c.Name, rc.RequestedModel) || strings.EqualFold(c.ID, rc.RequestedModel) {
			if strat, ok := mr.strategies["failover_combo"]; ok {
				return strat.SelectModel(ctx, rc)
			}
		}
	}

	// 4. Default: Direct routing
	if strat, ok := mr.strategies["direct"]; ok {
		return strat.SelectModel(ctx, rc)
	}

	direct := &DirectRoutingStrategy{}
	return direct.SelectModel(ctx, rc)
}
