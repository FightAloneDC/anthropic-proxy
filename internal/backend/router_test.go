package backend

import (
	"testing"

	"anthropic-proxy/internal/config"
)

func TestMatchModel(t *testing.T) {
	tests := []struct {
		pattern string
		model   string
		want    bool
	}{
		{"*", "mimo-v2", true},
		{"mimo-*", "mimo-v2", true},
		{"mimo-*", "deepseek-chat", false},
		{"*-embedding", "text-embedding", true},
		{"deepseek-chat", "deepseek-chat", true},
		{"deepseek-chat", "deepseek-coder", false},
	}
	for _, tt := range tests {
		if got := MatchModel(tt.pattern, tt.model); got != tt.want {
			t.Fatalf("MatchModel(%q, %q) = %v, want %v", tt.pattern, tt.model, got, tt.want)
		}
	}
}

func TestRouterCandidatesMatchModelAndNormalizeURL(t *testing.T) {
	router := NewRouter([]config.BackendConfig{
		{Name: "primary", URL: "https://a.example/v1", Models: []string{"mimo-*"}},
		{Name: "secondary", URL: "https://b.example", Models: []string{"deepseek-*"}},
	}, "round_robin")

	candidates := router.Candidates("deepseek-chat", "/v1/chat/completions", 0)
	if len(candidates) != 1 {
		t.Fatalf("candidates = %d", len(candidates))
	}
	if candidates[0].Runtime.Backend.Name != "secondary" {
		t.Fatalf("backend = %q", candidates[0].Runtime.Backend.Name)
	}
	if candidates[0].URL != "https://b.example/v1/chat/completions" {
		t.Fatalf("url = %q", candidates[0].URL)
	}
}

func TestRouterRoundRobin(t *testing.T) {
	router := NewRouter([]config.BackendConfig{
		{Name: "a", URL: "https://a.example", Models: []string{"mimo-*"}},
		{Name: "b", URL: "https://b.example", Models: []string{"mimo-*"}},
	}, "round_robin")

	first := router.Candidates("mimo-v2", "/v1/chat/completions", 0)[0].Runtime.Backend.Name
	second := router.Candidates("mimo-v2", "/v1/chat/completions", 0)[0].Runtime.Backend.Name
	if first == second {
		t.Fatalf("round robin did not rotate: first=%s second=%s", first, second)
	}
}

func TestRouterWeighted(t *testing.T) {
	router := NewRouter([]config.BackendConfig{
		{Name: "a", URL: "https://a.example", Models: []string{"mimo-*"}, Weight: 2},
		{Name: "b", URL: "https://b.example", Models: []string{"mimo-*"}, Weight: 1},
	}, "weighted")

	seen := map[string]int{}
	for i := 0; i < 3; i++ {
		name := router.Candidates("mimo-v2", "/v1/chat/completions", 0)[0].Runtime.Backend.Name
		seen[name]++
	}
	if seen["a"] <= seen["b"] {
		t.Fatalf("weighted distribution = %#v", seen)
	}
}

func TestRouterLeastConnections(t *testing.T) {
	router := NewRouter([]config.BackendConfig{
		{Name: "a", URL: "https://a.example", Models: []string{"mimo-*"}},
		{Name: "b", URL: "https://b.example", Models: []string{"mimo-*"}},
	}, "least_connections")
	router.Backends()[0].IncActive()

	candidate := router.Candidates("mimo-v2", "/v1/chat/completions", 0)[0]
	if candidate.Runtime.Backend.Name != "b" {
		t.Fatalf("backend = %q", candidate.Runtime.Backend.Name)
	}
}
