package proxy

import (
	"context"
	"testing"

	"9router-gateway/internal/entity"
	"9router-gateway/internal/upstream"
)

func TestDirectRoutingStrategy(t *testing.T) {
	strategy := &DirectRoutingStrategy{}
	rc := &RoutingContext{
		RequestedModel: "ag/gemini-3.8-flash",
	}

	decision, err := strategy.SelectModel(context.Background(), rc)
	if err != nil {
		t.Fatalf("expected direct route success, got %v", err)
	}
	if decision.SelectedModel != "ag/gemini-3.8-flash" {
		t.Errorf("expected ag/gemini-3.8-flash, got %s", decision.SelectedModel)
	}
}

func TestFailoverComboStrategy_CircuitBreakerSkip(t *testing.T) {
	breakers := NewCircuitBreakerRegistry(CircuitBreakerSettings{
		FailureThreshold: 2,
	})

	// Trip breaker for model #1
	cb1 := breakers.GetOrCreate("model-1")
	cb1.RecordFailure()
	cb1.RecordFailure()

	combos := []upstream.Combo{
		{
			ID:     "combo-main",
			Name:   "main",
			Models: []string{"model-1", "model-2", "model-3"},
		},
	}

	rc := &RoutingContext{
		RequestedModel: "main",
		Combos:         combos,
		Breakers:       breakers,
	}

	strategy := &FailoverComboStrategy{}
	decision, err := strategy.SelectModel(context.Background(), rc)
	if err != nil {
		t.Fatalf("expected failover to succeed, got %v", err)
	}

	// Model 1 was down, so it should automatically pick model-2
	if decision.SelectedModel != "model-2" {
		t.Errorf("expected failover to pick model-2, got %s", decision.SelectedModel)
	}
	if len(decision.FallbackChain) != 1 || decision.FallbackChain[0] != "model-3" {
		t.Errorf("expected fallback chain ['model-3'], got %v", decision.FallbackChain)
	}
}

func TestCostOptimizedStrategy(t *testing.T) {
	strategy := &CostOptimizedStrategy{}
	rc := &RoutingContext{
		RequestedModel: "cost-optimized",
		User: &entity.User{
			AllowedModels: `["*"]`,
		},
	}

	decision, err := strategy.SelectModel(context.Background(), rc)
	if err != nil {
		t.Fatalf("expected cost-optimized route to succeed, got %v", err)
	}
	if decision.SelectedModel != "oc/muse-spark-1.3-contributor-free" {
		t.Errorf("expected free tier model, got %s", decision.SelectedModel)
	}
}

func TestModelRouter_Dispatch(t *testing.T) {
	router := NewModelRouter()
	combos := []upstream.Combo{
		{
			ID:     "combo-1",
			Name:   "fast-combo",
			Models: []string{"model-fast-a", "model-fast-b"},
		},
	}

	// 1. Combo dispatch
	rc1 := &RoutingContext{
		RequestedModel: "fast-combo",
		Combos:         combos,
	}
	d1, err := router.Route(context.Background(), rc1)
	if err != nil || d1.SelectedModel != "model-fast-a" {
		t.Fatalf("expected combo route to model-fast-a, got %v (err: %v)", d1, err)
	}

	// 2. Free-only dispatch
	rc2 := &RoutingContext{
		RequestedModel: "free-only",
		User:           &entity.User{AllowedModels: `["*"]`},
	}
	d2, err := router.Route(context.Background(), rc2)
	if err != nil || d2.Strategy != "cost_optimized" {
		t.Fatalf("expected cost_optimized strategy, got %v (err: %v)", d2, err)
	}

	// 3. Direct model dispatch
	rc3 := &RoutingContext{
		RequestedModel: "claude-sonnet-4-6",
	}
	d3, err := router.Route(context.Background(), rc3)
	if err != nil || d3.SelectedModel != "claude-sonnet-4-6" {
		t.Fatalf("expected direct model, got %v (err: %v)", d3, err)
	}
}
