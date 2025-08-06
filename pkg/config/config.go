package config

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the complete application configuration
type Config struct {
	Ollama        OllamaConfig        `yaml:"ollama"`
	Research      ResearchConfig      `yaml:"research"`
	Tools         ToolsConfig         `yaml:"tools"`
	Storage       StorageConfig       `yaml:"storage"`
	API           APIConfig           `yaml:"api"`
	Observability ObservabilityConfig `yaml:"observability"`
}

// OllamaConfig contains Ollama-specific configuration
type OllamaConfig struct {
	BaseURL     string  `yaml:"base_url"`
	Model       string  `yaml:"model"`
	Temperature float64 `yaml:"temperature"`
	MaxTokens   int     `yaml:"max_tokens"`
	TopP        float64 `yaml:"top_p,omitempty"`
	TopK        int     `yaml:"top_k,omitempty"`
	Timeout     string  `yaml:"timeout"`
}

// ResearchConfig contains research workflow configuration
type ResearchConfig struct {
	MaxConcurrency   int     `yaml:"max_concurrency"`
	MaxIterations    int     `yaml:"max_iterations"`
	MaxDepth         int     `yaml:"max_depth"`
	CompressionRatio float64 `yaml:"compression_ratio"`
	Timeout          string  `yaml:"timeout"`
	MaxTasks         int     `yaml:"max_tasks"`
	TaskTimeout      string  `yaml:"task_timeout"`
}

// ToolsConfig contains tool-specific configuration
type ToolsConfig struct {
	WebSearch WebSearchConfig `yaml:"web_search"`
	Think     ThinkConfig     `yaml:"think"`
	Summarize SummarizeConfig `yaml:"summarize"`
}

// WebSearchConfig contains web search tool configuration
type WebSearchConfig struct {
	Enabled    bool   `yaml:"enabled"`
	Provider   string `yaml:"provider"`
	APIKey     string `yaml:"api_key,omitempty"`
	MaxResults int    `yaml:"max_results"`
	SafeSearch bool   `yaml:"safe_search"`
}

// ThinkConfig contains thinking tool configuration
type ThinkConfig struct {
	Enabled   bool `yaml:"enabled"`
	MaxDepth  int  `yaml:"max_depth"`
	MaxTokens int  `yaml:"max_tokens"`
}

// SummarizeConfig contains summarization tool configuration
type SummarizeConfig struct {
	Enabled   bool    `yaml:"enabled"`
	MaxLength int     `yaml:"max_length"`
	MinLength int     `yaml:"min_length"`
	Ratio     float64 `yaml:"ratio"`
}

// StorageConfig contains storage configuration
type StorageConfig struct {
	Type             string `yaml:"type"` // "memory", "file", "database"
	Path             string `yaml:"path,omitempty"`
	ConnectionString string `yaml:"connection_string,omitempty"`
	MaxSize          int    `yaml:"max_size,omitempty"`
	TTL              string `yaml:"ttl,omitempty"`
}

// APIConfig contains API server configuration
type APIConfig struct {
	Enabled        bool            `yaml:"enabled"`
	Port           int             `yaml:"port"`
	Host           string          `yaml:"host"`
	CORS           CORSConfig      `yaml:"cors"`
	RateLimit      RateLimitConfig `yaml:"rate_limit"`
	Authentication AuthConfig      `yaml:"authentication"`
}

// CORSConfig contains CORS configuration
type CORSConfig struct {
	AllowedOrigins []string `yaml:"allowed_origins"`
	AllowedMethods []string `yaml:"allowed_methods"`
	AllowedHeaders []string `yaml:"allowed_headers"`
	MaxAge         int      `yaml:"max_age"`
}

// RateLimitConfig contains rate limiting configuration
type RateLimitConfig struct {
	Enabled           bool `yaml:"enabled"`
	RequestsPerMinute int  `yaml:"requests_per_minute"`
	BurstSize         int  `yaml:"burst_size"`
}

// AuthConfig contains authentication configuration
type AuthConfig struct {
	Enabled   bool   `yaml:"enabled"`
	Type      string `yaml:"type"` // "none", "api_key", "jwt"
	APIKey    string `yaml:"api_key,omitempty"`
	JWTSecret string `yaml:"jwt_secret,omitempty"`
}

// ObservabilityConfig contains observability configuration
type ObservabilityConfig struct {
	Tracing TracingConfig `yaml:"tracing"`
	Metrics MetricsConfig `yaml:"metrics"`
	Logging LoggingConfig `yaml:"logging"`
}

// TracingConfig contains tracing configuration
type TracingConfig struct {
	Enabled      bool    `yaml:"enabled"`
	Provider     string  `yaml:"provider"` // "otlp", "jaeger", "zipkin"
	Endpoint     string  `yaml:"endpoint"`
	SamplingRate float64 `yaml:"sampling_rate"`
	Insecure     bool    `yaml:"insecure"`
}

// MetricsConfig contains metrics configuration
type MetricsConfig struct {
	Enabled      bool   `yaml:"enabled"`
	Provider     string `yaml:"provider"` // "prometheus", "otlp"
	Port         int    `yaml:"port"`
	Endpoint     string `yaml:"endpoint,omitempty"`
	PushInterval string `yaml:"push_interval,omitempty"`
}

// LoggingConfig contains logging configuration
type LoggingConfig struct {
	Level      string `yaml:"level"`  // "debug", "info", "warn", "error"
	Format     string `yaml:"format"` // "json", "text"
	Output     string `yaml:"output"` // "stdout", "file"
	FilePath   string `yaml:"file_path,omitempty"`
	MaxSize    int    `yaml:"max_size,omitempty"`
	MaxBackups int    `yaml:"max_backups,omitempty"`
	MaxAge     int    `yaml:"max_age,omitempty"`
}

// Load loads configuration from a file
func Load(path string) (*Config, error) {
	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("config file not found: %s", path)
	}

	// Read file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Parse YAML
	config := &Config{}
	if err := yaml.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Apply defaults
	config.applyDefaults()

	// Override with environment variables
	config.overrideFromEnv()

	// Validate configuration
	if err := config.validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return config, nil
}

// LoadOrDefault loads configuration from a file or returns default config
func LoadOrDefault(path string) *Config {
	config, err := Load(path)
	if err != nil {
		config = Default()
	}
	return config
}

// Default returns default configuration
func Default() *Config {
	return &Config{
		Ollama: OllamaConfig{
			BaseURL:     "http://localhost:11434",
			Model:       "llama3.2",
			Temperature: 0.7,
			MaxTokens:   4096,
			Timeout:     "2m",
		},
		Research: ResearchConfig{
			MaxConcurrency:   5,
			MaxIterations:    6,
			MaxDepth:         3,
			CompressionRatio: 0.3,
			Timeout:          "5m",
			MaxTasks:         50,
			TaskTimeout:      "2m",
		},
		Tools: ToolsConfig{
			WebSearch: WebSearchConfig{
				Enabled:    true,
				Provider:   "duckduckgo",
				MaxResults: 5,
				SafeSearch: true,
			},
			Think: ThinkConfig{
				Enabled:   true,
				MaxDepth:  3,
				MaxTokens: 1000,
			},
			Summarize: SummarizeConfig{
				Enabled:   true,
				MaxLength: 500,
				MinLength: 100,
				Ratio:     0.3,
			},
		},
		Storage: StorageConfig{
			Type:    "memory",
			Path:    "./data",
			MaxSize: 1000,
			TTL:     "24h",
		},
		API: APIConfig{
			Enabled: false,
			Port:    8080,
			Host:    "0.0.0.0",
			CORS: CORSConfig{
				AllowedOrigins: []string{"*"},
				AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
				AllowedHeaders: []string{"*"},
				MaxAge:         3600,
			},
			RateLimit: RateLimitConfig{
				Enabled:           false,
				RequestsPerMinute: 60,
				BurstSize:         10,
			},
			Authentication: AuthConfig{
				Enabled: false,
				Type:    "none",
			},
		},
		Observability: ObservabilityConfig{
			Tracing: TracingConfig{
				Enabled:      true,
				Provider:     "otlp",
				Endpoint:     "localhost:4317",
				SamplingRate: 1.0,
				Insecure:     true,
			},
			Metrics: MetricsConfig{
				Enabled:      true,
				Provider:     "prometheus",
				Port:         2223,
				PushInterval: "10s",
			},
			Logging: LoggingConfig{
				Level:      "info",
				Format:     "json",
				Output:     "stdout",
				MaxSize:    100,
				MaxBackups: 3,
				MaxAge:     7,
			},
		},
	}
}

// applyDefaults applies default values to missing fields
func (c *Config) applyDefaults() {
	defaults := Default()

	// Apply Ollama defaults
	if c.Ollama.BaseURL == "" {
		c.Ollama.BaseURL = defaults.Ollama.BaseURL
	}
	if c.Ollama.Model == "" {
		c.Ollama.Model = defaults.Ollama.Model
	}
	if c.Ollama.Temperature == 0 {
		c.Ollama.Temperature = defaults.Ollama.Temperature
	}
	if c.Ollama.MaxTokens == 0 {
		c.Ollama.MaxTokens = defaults.Ollama.MaxTokens
	}

	// Apply Research defaults
	if c.Research.MaxConcurrency == 0 {
		c.Research.MaxConcurrency = defaults.Research.MaxConcurrency
	}
	if c.Research.MaxIterations == 0 {
		c.Research.MaxIterations = defaults.Research.MaxIterations
	}

	// Apply other defaults as needed...
}

// overrideFromEnv overrides configuration from environment variables
func (c *Config) overrideFromEnv() {
	// Ollama overrides
	if url := os.Getenv("OLLAMA_BASE_URL"); url != "" {
		c.Ollama.BaseURL = url
	}
	if model := os.Getenv("OLLAMA_MODEL"); model != "" {
		c.Ollama.Model = model
	}

	// API overrides
	if port := os.Getenv("API_PORT"); port != "" {
		_, err := fmt.Sscanf(port, "%d", &c.API.Port)
		if err != nil {
			log.Printf("Invalid API_PORT value: %s, using default: %d", port, c.API.Port)
		}
	}

	// Observability overrides
	if endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"); endpoint != "" {
		c.Observability.Tracing.Endpoint = endpoint
	}

	// Add more environment variable overrides as needed...
}

// validate validates the configuration
func (c *Config) validate() error {
	// Validate Ollama config
	if c.Ollama.BaseURL == "" {
		return fmt.Errorf("ollama base_url is required")
	}
	if c.Ollama.Model == "" {
		return fmt.Errorf("ollama model is required")
	}

	// Validate Research config
	if c.Research.MaxConcurrency < 1 {
		return fmt.Errorf("research max_concurrency must be at least 1")
	}
	if c.Research.MaxIterations < 1 {
		return fmt.Errorf("research max_iterations must be at least 1")
	}

	// Validate API config
	if c.API.Enabled && (c.API.Port < 1 || c.API.Port > 65535) {
		return fmt.Errorf("api port must be between 1 and 65535")
	}

	// Validate timeout strings
	if _, err := time.ParseDuration(c.Research.Timeout); err != nil {
		return fmt.Errorf("invalid research timeout: %w", err)
	}

	return nil
}

// Save saves the configuration to a file
func (c *Config) Save(path string) error {
	// Create directory if it doesn't exist
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Marshal to YAML
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write to file
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// GetDuration parses a duration string from config
func (c *Config) GetDuration(value string) (time.Duration, error) {
	return time.ParseDuration(value)
}

// IsProduction returns true if running in production environment
func (c *Config) IsProduction() bool {
	env := os.Getenv("ENVIRONMENT")
	return strings.ToLower(env) == "production" || strings.ToLower(env) == "prod"
}
