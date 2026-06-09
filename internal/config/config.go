package config

import (
	"flag"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config represents the application configuration
type Config struct {
	Server   ServerConfig    `yaml:"server"`
	Backend  BackendConfig   `yaml:"backend"`
	Backends []BackendConfig `yaml:"backends"`
	Proxy    ProxyConfig     `yaml:"proxy"`
	Models   []ModelMap      `yaml:"models"`
}

// ServerConfig holds server-related settings
type ServerConfig struct {
	Port int    `yaml:"port"`
	Fg   bool   `yaml:"fg"`
	Log  string `yaml:"log,omitempty"`
}

// BackendConfig holds backend connection settings
type BackendConfig struct {
	Name      string           `yaml:"name"`
	URL       string           `yaml:"url"`
	APIKey    string           `yaml:"api_key"`
	Models    []string         `yaml:"models"`
	Weight    int              `yaml:"weight"`
	Priority  int              `yaml:"priority"`
	Enabled   *bool            `yaml:"enabled"`
	Overrides BackendOverrides `yaml:"overrides"`
}

// BackendOverrides holds backend-specific JSON request compatibility overrides.
type BackendOverrides struct {
	DropFields []string `yaml:"drop_fields"`
}

// ProxyConfig holds proxy behavior settings
type ProxyConfig struct {
	SkipThinking                   bool   `yaml:"skip_thinking"`
	Debug                          bool   `yaml:"debug"`
	StoreBackend                   string `yaml:"store_backend"`     // memory or file, default memory
	StoreFile                      string `yaml:"store_file"`        // default ./data/responses.jsonl
	StoreTTL                       int    `yaml:"store_ttl"`         // seconds, default 3600
	StoreMaxEntries                int    `yaml:"store_max_entries"` // default 1000
	LogFormat                      string `yaml:"log_format"`        // "text" or "json"
	LogLevel                       string `yaml:"log_level"`         // debug, info, warn, error
	MetricsEnabled                 *bool  `yaml:"metrics_enabled"`   // default true
	HealthBackendCheck             bool   `yaml:"health_backend_check"`
	BackendHealthEnabled           bool   `yaml:"backend_health_enabled"`
	BackendHealthInterval          int    `yaml:"backend_health_interval"`
	BackendHealthTimeout           int    `yaml:"backend_health_timeout"`
	CircuitBreakerEnabled          bool   `yaml:"circuit_breaker_enabled"`
	CircuitBreakerFailureThreshold int    `yaml:"circuit_breaker_failure_threshold"`
	CircuitBreakerCooldown         int    `yaml:"circuit_breaker_cooldown"`
	RetryEnabled                   bool   `yaml:"retry_enabled"`
	RetryMaxAttempts               int    `yaml:"retry_max_attempts"`
	RetryInitialBackoffMS          int    `yaml:"retry_initial_backoff_ms"`
	RetryMaxBackoffMS              int    `yaml:"retry_max_backoff_ms"`
	RateLimitEnabled               bool   `yaml:"rate_limit_enabled"`
	RateLimitRequestsPerMinute     int    `yaml:"rate_limit_requests_per_minute"`
	RateLimitBurst                 int    `yaml:"rate_limit_burst"`
	LoadBalanceStrategy            string `yaml:"load_balance_strategy"`
	FailoverEnabled                bool   `yaml:"failover_enabled"`
	FailoverMaxBackends            int    `yaml:"failover_max_backends"`
}

// ModelMap represents a single model mapping
type ModelMap struct {
	From string `yaml:"from"`
	To   string `yaml:"to"`
}

// GetModelMap returns the model mapping as a map[string]string
func (c *Config) GetModelMap() map[string]string {
	m := make(map[string]string)
	for _, mm := range c.Models {
		m[mm.From] = mm.To
	}
	return m
}

// Load loads configuration from YAML file with CLI flag overrides
func Load() (*Config, error) {
	// Define CLI flags
	configPath := flag.String("config", "config.yaml", "path to config file")
	port := flag.Int("port", 0, "listen port (overrides config)")
	baseURL := flag.String("url", "", "backend URL (overrides config)")
	apiKey := flag.String("key", "", "backend API key (overrides config)")
	skipThinking := flag.Bool("skip-thinking", false, "skip thinking blocks (overrides config)")
	debugFlag := flag.Bool("debug", false, "enable debug logging (overrides config)")
	fg := flag.Bool("fg", false, "run in foreground (overrides config)")
	logFile := flag.String("log-file", "", "write logs to this file path (overrides config)")
	flag.Parse()

	// Load config file
	metricsEnabled := true
	cfg := &Config{
		Server: ServerConfig{
			Port: 8006,
		},
		Backend: BackendConfig{},
		Proxy: ProxyConfig{
			StoreBackend:                   "memory",
			StoreFile:                      "./data/responses.jsonl",
			LogFormat:                      "text",
			LogLevel:                       "info",
			MetricsEnabled:                 &metricsEnabled,
			CircuitBreakerEnabled:          true,
			CircuitBreakerFailureThreshold: 3,
			CircuitBreakerCooldown:         30,
			RetryEnabled:                   true,
			RetryMaxAttempts:               3,
			RetryInitialBackoffMS:          200,
			RetryMaxBackoffMS:              2000,
			RateLimitRequestsPerMinute:     60,
			RateLimitBurst:                 20,
			LoadBalanceStrategy:            "round_robin",
			FailoverEnabled:                true,
		},
	}

	if err := loadFile(*configPath, cfg); err != nil {
		// If file not found and no explicit config flag, try env vars
		if *configPath == "config.yaml" {
			loadFromEnv(cfg)
		} else {
			return nil, fmt.Errorf("loading config file: %w", err)
		}
	}

	// Apply CLI flag overrides
	if *port > 0 {
		cfg.Server.Port = *port
	}
	if *baseURL != "" {
		cfg.Backend.URL = *baseURL
	}
	if *apiKey != "" {
		cfg.Backend.APIKey = *apiKey
	}
	if *skipThinking {
		cfg.Proxy.SkipThinking = true
	}
	if *debugFlag {
		cfg.Proxy.Debug = true
	}
	if *fg {
		cfg.Server.Fg = true
	}
	if *logFile != "" {
		cfg.Server.Log = *logFile
	}

	if cfg.Proxy.LogFormat == "" {
		cfg.Proxy.LogFormat = "text"
	}
	if cfg.Proxy.LogLevel == "" {
		cfg.Proxy.LogLevel = "info"
	}
	if cfg.Proxy.StoreBackend == "" {
		cfg.Proxy.StoreBackend = "memory"
	}
	if cfg.Proxy.StoreFile == "" {
		cfg.Proxy.StoreFile = "./data/responses.jsonl"
	}
	if cfg.Proxy.MetricsEnabled == nil {
		enabled := true
		cfg.Proxy.MetricsEnabled = &enabled
	}
	if cfg.Proxy.CircuitBreakerFailureThreshold <= 0 {
		cfg.Proxy.CircuitBreakerFailureThreshold = 3
	}
	if cfg.Proxy.CircuitBreakerCooldown <= 0 {
		cfg.Proxy.CircuitBreakerCooldown = 30
	}
	if cfg.Proxy.BackendHealthInterval <= 0 {
		cfg.Proxy.BackendHealthInterval = 30
	}
	if cfg.Proxy.BackendHealthTimeout <= 0 {
		cfg.Proxy.BackendHealthTimeout = 5
	}
	if cfg.Proxy.RetryMaxAttempts <= 0 {
		cfg.Proxy.RetryMaxAttempts = 3
	}
	if cfg.Proxy.RetryInitialBackoffMS <= 0 {
		cfg.Proxy.RetryInitialBackoffMS = 200
	}
	if cfg.Proxy.RetryMaxBackoffMS <= 0 {
		cfg.Proxy.RetryMaxBackoffMS = 2000
	}
	if cfg.Proxy.RateLimitRequestsPerMinute <= 0 {
		cfg.Proxy.RateLimitRequestsPerMinute = 60
	}
	if cfg.Proxy.RateLimitBurst <= 0 {
		cfg.Proxy.RateLimitBurst = 20
	}
	if cfg.Proxy.LoadBalanceStrategy == "" {
		cfg.Proxy.LoadBalanceStrategy = "round_robin"
	}

	// Also check env vars as fallback
	if cfg.Backend.URL == "" {
		if envURL := os.Getenv("OPENAI_BASE_URL"); envURL != "" {
			cfg.Backend.URL = envURL
		}
	}
	if cfg.Backend.APIKey == "" {
		if envKey := os.Getenv("OPENAI_API_KEY"); envKey != "" {
			cfg.Backend.APIKey = envKey
		}
	}

	if err := cfg.NormalizeBackends(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// NormalizeBackends applies defaults and validates configured backends.
func (c *Config) NormalizeBackends() error {
	if c.Proxy.LoadBalanceStrategy == "" {
		c.Proxy.LoadBalanceStrategy = "round_robin"
	}
	if len(c.Backends) == 0 {
		if c.Backend.URL == "" {
			return fmt.Errorf("backend url is required")
		}
		c.Backends = []BackendConfig{{
			Name:   "default",
			URL:    c.Backend.URL,
			APIKey: c.Backend.APIKey,
			Models: []string{"*"},
			Weight: 1,
		}}
		return nil
	}
	seen := map[string]bool{}
	for i := range c.Backends {
		backend := &c.Backends[i]
		if backend.Name == "" {
			return fmt.Errorf("backend name is required")
		}
		if seen[backend.Name] {
			return fmt.Errorf("duplicate backend name: %s", backend.Name)
		}
		seen[backend.Name] = true
		if backend.URL == "" {
			return fmt.Errorf("backend %s url is required", backend.Name)
		}
		if len(backend.Models) == 0 {
			backend.Models = []string{"*"}
		}
		if backend.Weight <= 0 {
			backend.Weight = 1
		}
	}
	return nil
}

// EffectiveBackends returns the normalized backend list.
func (c *Config) EffectiveBackends() []BackendConfig {
	if len(c.Backends) == 0 {
		_ = c.NormalizeBackends()
	}
	return c.Backends
}

func loadFile(path string, cfg *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, cfg)
}

func loadFromEnv(cfg *Config) {
	if port := os.Getenv("PORT"); port != "" {
		fmt.Sscanf(port, "%d", &cfg.Server.Port)
	}
	if url := os.Getenv("OPENAI_BASE_URL"); url != "" {
		cfg.Backend.URL = url
	}
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		cfg.Backend.APIKey = key
	}
	if models := os.Getenv("MODEL_MAP"); models != "" {
		cfg.Models = parseModelMapString(models)
	}
}

// parseModelMapString parses "from1:to1,from2:to2" format
func parseModelMapString(s string) []ModelMap {
	var result []ModelMap
	for _, pair := range splitCSV(s) {
		parts := splitN(pair, ":", 2)
		if len(parts) == 2 {
			result = append(result, ModelMap{From: parts[0], To: parts[1]})
		}
	}
	return result
}

func splitCSV(s string) []string {
	var result []string
	current := ""
	for _, c := range s {
		if c == ',' {
			if current != "" {
				result = append(result, current)
			}
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}

func splitN(s, sep string, n int) []string {
	parts := make([]string, 0, n)
	idx := 0
	for i := 0; i < n-1; i++ {
		pos := findSep(s[idx:], sep)
		if pos < 0 {
			break
		}
		parts = append(parts, s[idx:idx+pos])
		idx += pos + len(sep)
	}
	parts = append(parts, s[idx:])
	return parts
}

func findSep(s, sep string) int {
	for i := 0; i <= len(s)-len(sep); i++ {
		if s[i:i+len(sep)] == sep {
			return i
		}
	}
	return -1
}
