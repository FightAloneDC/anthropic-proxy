package config

import (
	"flag"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config represents the application configuration
type Config struct {
	Server  ServerConfig  `yaml:"server"`
	Backend BackendConfig `yaml:"backend"`
	Proxy   ProxyConfig   `yaml:"proxy"`
	Models  []ModelMap    `yaml:"models"`
}

// ServerConfig holds server-related settings
type ServerConfig struct {
	Port int    `yaml:"port"`
	Fg   bool   `yaml:"fg"`
	Log  string `yaml:"log,omitempty"`
}

// BackendConfig holds backend connection settings
type BackendConfig struct {
	URL    string `yaml:"url"`
	APIKey string `yaml:"api_key"`
}

// ProxyConfig holds proxy behavior settings
type ProxyConfig struct {
	SkipThinking bool `yaml:"skip_thinking"`
	Debug        bool `yaml:"debug"`
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
	flag.Parse()

	// Load config file
	cfg := &Config{
		Server: ServerConfig{
			Port: 8006,
		},
		Backend: BackendConfig{
			URL: "http://localhost:11434",
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

	// Also check env vars as fallback
	if cfg.Backend.URL == "http://localhost:11434" {
		if envURL := os.Getenv("OPENAI_BASE_URL"); envURL != "" {
			cfg.Backend.URL = envURL
		}
	}
	if cfg.Backend.APIKey == "" {
		if envKey := os.Getenv("OPENAI_API_KEY"); envKey != "" {
			cfg.Backend.APIKey = envKey
		}
	}

	return cfg, nil
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
