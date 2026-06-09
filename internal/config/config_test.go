package config

import "testing"

func TestNormalizeBackendsRequiresBackendURL(t *testing.T) {
	cfg := &Config{}
	if err := cfg.NormalizeBackends(); err == nil {
		t.Fatal("NormalizeBackends succeeded without backend url")
	}
}

func TestNormalizeBackendsDoesNotDefaultToOllama(t *testing.T) {
	cfg := &Config{Backend: BackendConfig{URL: "https://backend.example/v1"}}
	if err := cfg.NormalizeBackends(); err != nil {
		t.Fatalf("NormalizeBackends: %v", err)
	}
	if cfg.Backends[0].URL == "http://localhost:11434" {
		t.Fatal("unexpected Ollama default backend")
	}
}

func TestNormalizeBackendsCreatesDefaultBackend(t *testing.T) {
	cfg := &Config{Backend: BackendConfig{URL: "https://backend.example/v1", APIKey: "key"}}
	if err := cfg.NormalizeBackends(); err != nil {
		t.Fatalf("NormalizeBackends: %v", err)
	}
	backends := cfg.EffectiveBackends()
	if len(backends) != 1 {
		t.Fatalf("backends = %d", len(backends))
	}
	if backends[0].Name != "default" || backends[0].URL != "https://backend.example/v1" || backends[0].APIKey != "key" {
		t.Fatalf("backend = %#v", backends[0])
	}
	if len(backends[0].Models) != 1 || backends[0].Models[0] != "*" {
		t.Fatalf("models = %#v", backends[0].Models)
	}
}

func TestNormalizeBackendsAppliesDefaults(t *testing.T) {
	cfg := &Config{Backends: []BackendConfig{{Name: "primary", URL: "https://backend.example/v1"}}}
	if err := cfg.NormalizeBackends(); err != nil {
		t.Fatalf("NormalizeBackends: %v", err)
	}
	backend := cfg.Backends[0]
	if backend.Weight != 1 {
		t.Fatalf("weight = %d", backend.Weight)
	}
	if len(backend.Models) != 1 || backend.Models[0] != "*" {
		t.Fatalf("models = %#v", backend.Models)
	}
	if cfg.Proxy.LoadBalanceStrategy != "round_robin" {
		t.Fatalf("strategy = %q", cfg.Proxy.LoadBalanceStrategy)
	}
	wantStatusCodes := []int{429, 502, 503, 504}
	if len(cfg.Proxy.FailoverStatusCodes) != len(wantStatusCodes) {
		t.Fatalf("failover status codes = %#v", cfg.Proxy.FailoverStatusCodes)
	}
	for i, want := range wantStatusCodes {
		if cfg.Proxy.FailoverStatusCodes[i] != want {
			t.Fatalf("failover status codes = %#v", cfg.Proxy.FailoverStatusCodes)
		}
	}
}

func TestNormalizeBackendsRejectsInvalidBackends(t *testing.T) {
	tests := []Config{
		{Backends: []BackendConfig{{URL: "https://backend.example/v1"}}},
		{Backends: []BackendConfig{{Name: "primary"}}},
		{Backends: []BackendConfig{{Name: "primary", URL: "https://a.example"}, {Name: "primary", URL: "https://b.example"}}},
	}
	for _, cfg := range tests {
		if err := cfg.NormalizeBackends(); err == nil {
			t.Fatalf("NormalizeBackends succeeded for %#v", cfg.Backends)
		}
	}
}
