package http

import (
	"encoding/json"
	"testing"

	"9router-gateway/web"
)

// ponytail: validates the embedded spec stays parseable and covers the public surface.
// Add path assertions here when new public endpoints are added.
func TestOpenAPISpecEmbedded(t *testing.T) {
	raw, err := web.FS.ReadFile("static/openapi.json")
	if err != nil {
		t.Fatalf("read embedded openapi.json: %v", err)
	}
	var spec struct {
		OpenAPI string                    `json:"openapi"`
		Paths   map[string]json.RawMessage `json:"paths"`
		Servers []struct {
			URL string `json:"url"`
		} `json:"servers"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("openapi.json is not valid JSON: %v", err)
	}
	if spec.OpenAPI == "" {
		t.Fatal("openapi.json missing version field")
	}
	for _, p := range []string{"/chat/completions", "/models", "/messages", "/embeddings", "/healthz", "/version"} {
		if _, ok := spec.Paths[p]; !ok {
			t.Errorf("openapi.json missing path %s", p)
		}
	}
	if len(spec.Servers) == 0 || spec.Servers[0].URL != "/" {
		t.Errorf("expected relative server url \"/\", got %+v", spec.Servers)
	}
}
