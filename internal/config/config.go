package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config holds application configuration
type Config struct {
	Server       ServerConfig       `yaml:"server"`
	CORS         CORSConfig         `yaml:"cors"`
	Resiliency   ResiliencyConfig   `yaml:"resiliency"`
	Storage      StorageConfig      `yaml:"storage"`
	Baseline     BaselineConfig     `yaml:"baseline"`
	AIAnalyzer   AIAnalyzerConfig   `yaml:"ai_analyzer"`
	VocabAgent   VocabAgentConfig   `yaml:"vocab_agent"`
	ContextAgent ContextAgentConfig `yaml:"context_agent"`
	Security     SecurityConfig     `yaml:"security"`
	DataMapping  DataMappingConfig  `yaml:"data_mapping"`
}

// DataMappingConfig holds declarative data mapping configuration
type DataMappingConfig struct {
	Enabled bool                        `yaml:"enabled"`
	Mapping map[string]FieldMappingSpec `yaml:"mapping"`
}

// FieldMappingSpec defines how to extract a field from raw events
// Can be specified as:
//   - Simple: paths only (list of fallback paths)
//   - Complex: paths + format + required flag
type FieldMappingSpec struct {
	Paths    []string `yaml:"paths"`              // Fallback paths to try (e.g., "payload.event.action")
	Format   string   `yaml:"format,omitempty"`   // For timestamps: epoch_ms, epoch_s, iso8601
	Required bool     `yaml:"required,omitempty"` // If true, error when all paths fail
}

// ServerConfig holds server-related configuration
type ServerConfig struct {
	Port            string `yaml:"port"`
	Host            string `yaml:"host"`
	Environment     string `yaml:"environment"`
	ReadTimeout     string `yaml:"read_timeout"`
	WriteTimeout    string `yaml:"write_timeout"`
	IdleTimeout     string `yaml:"idle_timeout"`
	ShutdownTimeout string `yaml:"shutdown_timeout"`
}

// CORSConfig holds CORS-related configuration
type CORSConfig struct {
	AllowedOrigins []string `yaml:"allowed_origins"`
	AllowedMethods []string `yaml:"allowed_methods"`
	AllowedHeaders []string `yaml:"allowed_headers"`
}

// ResiliencyConfig holds resiliency-related configuration
type ResiliencyConfig struct {
	RateLimitRequests int                  `yaml:"rate_limit_requests"` // Requests per second
	CircuitBreaker    CircuitBreakerConfig `yaml:"circuit_breaker"`
}

// CircuitBreakerConfig holds circuit breaker configuration for AI layer
type CircuitBreakerConfig struct {
	Enabled          bool   `yaml:"enabled"`
	FailureThreshold int    `yaml:"failure_threshold"` // Number of failures before opening
	ResetTimeout     string `yaml:"reset_timeout"`     // Time to wait before trying again
}

// StorageConfig holds storage-related configuration
type StorageConfig struct {
	Type          string                `yaml:"type"` // "postgres"
	RetentionDays int                   `yaml:"retention_days"`
	Postgres      PostgresStorageConfig `yaml:"postgres"`
}

// PostgresStorageConfig holds PostgreSQL-specific configuration
type PostgresStorageConfig struct {
	Host           string `yaml:"host"`
	Port           int    `yaml:"port"`
	Database       string `yaml:"database"`
	User           string `yaml:"user"`
	Password       string `yaml:"password"`
	SSLMode        string `yaml:"ssl_mode"`
	MaxConnections int    `yaml:"max_connections"`
}

// BaselineConfig holds baseline detection configuration
type BaselineConfig struct {
	WindowDays                int     `yaml:"window_days"`
	MinDays                   int     `yaml:"min_days"`
	ComputationMode           string  `yaml:"computation_mode"` // "daily" or "always"
	TrendThreshold            float64 `yaml:"trend_threshold"`
	HighSignificanceThreshold float64 `yaml:"high_significance_threshold"`
	StdDeviationMultiplier    float64 `yaml:"std_deviation_multiplier"`
}

// AIAnalyzerConfig holds AI analyzer layer configuration (for baseline analysis summaries)
type AIAnalyzerConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Provider string `yaml:"provider"`
	APIKey   string `yaml:"api_key"`
	Model    string `yaml:"model"`
}

// VocabAgentConfig holds vocab agent configuration (for classifying unknown words)
type VocabAgentConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Provider string `yaml:"provider"`
	APIKey   string `yaml:"api_key"`
	Model    string `yaml:"model"`
}

// ContextAgentConfig holds context agent configuration (for classifying event properties)
type ContextAgentConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Provider string `yaml:"provider"`
	APIKey   string `yaml:"api_key"`
	Model    string `yaml:"model"`
}

// SecurityConfig holds security-related configuration
type SecurityConfig struct {
	Enabled    bool   `yaml:"enabled"`
	APIKeyHash string `yaml:"api_key_hash"` // SHA256 hash of the API key
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Port:            "8080",
			Host:            "0.0.0.0",
			Environment:     "development",
			ReadTimeout:     "10s",
			WriteTimeout:    "30s",
			IdleTimeout:     "60s",
			ShutdownTimeout: "15s",
		},
		CORS: CORSConfig{
			AllowedOrigins: []string{"*"},
			AllowedMethods: []string{"GET", "POST", "OPTIONS"},
			AllowedHeaders: []string{"Content-Type", "Authorization"},
		},
		Resiliency: ResiliencyConfig{
			RateLimitRequests: 100,
			CircuitBreaker: CircuitBreakerConfig{
				Enabled:          true,
				FailureThreshold: 5,
				ResetTimeout:     "30s",
			},
		},
		Storage: StorageConfig{
			Type:          "postgres",
			RetentionDays: 90,
		},
		Baseline: BaselineConfig{
			WindowDays:                28,
			MinDays:                   7,
			ComputationMode:           "daily",
			TrendThreshold:            0.10,
			HighSignificanceThreshold: 0.15,
			StdDeviationMultiplier:    2.0,
		},
		AIAnalyzer: AIAnalyzerConfig{
			Enabled:  false,
			Provider: "groq",
			APIKey:   "",
			Model:    "llama-3.1-8b-instant",
		},
		VocabAgent: VocabAgentConfig{
			Enabled:  false,
			Provider: "groq",
			APIKey:   "",
			Model:    "llama-3.1-8b-instant",
		},
		ContextAgent: ContextAgentConfig{
			Enabled:  false,
			Provider: "groq",
			APIKey:   "",
			Model:    "llama-3.1-8b-instant",
		},
		Security: SecurityConfig{
			Enabled:    false,
			APIKeyHash: "",
		},
		DataMapping: DataMappingConfig{
			Enabled: false,
			Mapping: nil,
		},
	}
}

// Load reads configuration from config.yaml file
// Falls back to environment variables, then defaults
func Load() *Config {
	cfg := DefaultConfig()

	// Try to load from config file
	configPaths := []string{
		"config.yaml",
		"config.yml",
		"/etc/velum/config.yaml",
	}

	var loaded bool
	for _, path := range configPaths {
		if data, err := os.ReadFile(path); err == nil {
			if err := yaml.Unmarshal(data, cfg); err != nil {
				fmt.Printf("Warning: Failed to parse %s: %v\n", path, err)
				continue
			}
			fmt.Printf("Loaded configuration from %s\n", path)
			loaded = true
			break
		}
	}

	if !loaded {
		fmt.Println("No config file found, using defaults")
	}

	// Environment variables override config file
	if port := os.Getenv("VELUM_PORT"); port != "" {
		cfg.Server.Port = port
	}
	if env := os.Getenv("VELUM_ENV"); env != "" {
		cfg.Server.Environment = env
	}
	if aiKey := os.Getenv("VELUM_AI_API_KEY"); aiKey != "" {
		cfg.AIAnalyzer.APIKey = aiKey
	}
	if vocabKey := os.Getenv("VELUM_VOCAB_AGENT_API_KEY"); vocabKey != "" {
		cfg.VocabAgent.APIKey = vocabKey
	}
	if ctxKey := os.Getenv("VELUM_CONTEXT_AGENT_API_KEY"); ctxKey != "" {
		cfg.ContextAgent.APIKey = ctxKey
	}

	return cfg
}

// LoadFromFile loads configuration from a specific file path
func LoadFromFile(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return cfg, nil
}
