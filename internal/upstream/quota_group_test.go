package upstream

import "testing"

func TestGroupByProvider(t *testing.T) {
	r := &UpstreamQuotaReport{Accounts: []ProviderAccount{
		{Provider: "kiro", Name: "k1"},
		{Provider: "antigravity", Name: "a1"},
		{Provider: "kiro", Name: "k2"},
	}}
	g := r.GroupByProvider()
	if len(g) != 2 {
		t.Fatalf("groups=%d want 2", len(g))
	}
	if g[0].Provider != "kiro" || len(g[0].Accounts) != 2 || g[0].Accounts[0].Name != "k1" || g[0].Accounts[1].Name != "k2" {
		t.Fatalf("kiro group wrong: %+v", g[0])
	}
	if g[1].Provider != "antigravity" || len(g[1].Accounts) != 1 {
		t.Fatalf("antigravity group wrong: %+v", g[1])
	}
	var nilR *UpstreamQuotaReport
	if nilR.GroupByProvider() != nil {
		t.Fatal("nil report should return nil")
	}
}
