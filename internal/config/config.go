// Package config handles loading and validating Velum's application configuration.
//
// Configuration is resolved in this order (last wins):
//  1. Compiled defaults (DefaultConfig)
//  2. config.yaml / config.yml / /etc/velum/config.yaml
//  3. Environment variables (VELUM_*)
package config

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Root
// ---------------------------------------------------------------------------

// Config is the top-level configuration for the Velum server.
type Config struct {
	Server       ServerConfig       `yaml:"server"`
	Storage      StorageConfig      `yaml:"storage"`
	Security     SecurityConfig     `yaml:"security"`
	CORS         CORSConfig         `yaml:"cors"`
	Resiliency   ResiliencyConfig   `yaml:"resiliency"`
	Baseline     BaselineConfig     `yaml:"baseline"`
	AIAnalyzer   AIAnalyzerConfig   `yaml:"ai_analyzer"`
	VocabAgent   VocabAgentConfig   `yaml:"vocab_agent"`
	ContextAgent ContextAgentConfig `yaml:"context_agent"`
	DataMapping  DataMappingConfig  `yaml:"data_mapping"`
}

// ---------------------------------------------------------------------------
// Server
// ---------------------------------------------------------------------------

// ServerConfig controls the HTTP server and runtime environment.
type ServerConfig struct {
	Port            string `yaml:"port"`             // Listen port (default "8080")
	Host            string `yaml:"host"`             // Bind address: "0.0.0.0" (all) or "127.0.0.1" (local)
	Environment     string `yaml:"environment"`      // "development" | "staging" | "production"
	ReadTimeout     string `yaml:"read_timeout"`     // Max time to read request body
	WriteTimeout    string `yaml:"write_timeout"`    // Max time to write response
	IdleTimeout     string `yaml:"idle_timeout"`     // Keep-alive connection limit
	ShutdownTimeout string `yaml:"shutdown_timeout"` // Graceful shutdown wait time
}

// ---------------------------------------------------------------------------
// Storage
// ---------------------------------------------------------------------------

// StorageConfig selects the persistence backend and retention policy.
type StorageConfig struct {
	Type          string                `yaml:"type"`           // Backend type: "postgres"
	RetentionDays int                   `yaml:"retention_days"` // Auto-delete snapshots older than this (days)
	Postgres      PostgresStorageConfig `yaml:"postgres"`
}

// PostgresStorageConfig holds PostgreSQL connection parameters.
type PostgresStorageConfig struct {
	Host           string `yaml:"host"`
	Port           int    `yaml:"port"`
	Database       string `yaml:"database"`
	User           string `yaml:"user"`
	Password       string `yaml:"password"`
	SSLMode        string `yaml:"ssl_mode"`        // "prefer" | "require" | "disable"
	MaxConnections int    `yaml:"max_connections"` // Connection pool size
}

// ---------------------------------------------------------------------------
// Security
// ---------------------------------------------------------------------------

// SecurityConfig enables API key authentication on protected routes.
// When enabled, every request must include the X-Infra-Key header whose
// SHA-256 hash matches APIKeyHash.
type SecurityConfig struct {
	Enabled    bool   `yaml:"enabled"`
	APIKeyHash string `yaml:"api_key_hash"` // SHA-256 hex digest of the raw API key
}

// ---------------------------------------------------------------------------
// CORS
// ---------------------------------------------------------------------------

// CORSConfig controls Cross-Origin Resource Sharing headers.
type CORSConfig struct {
	AllowedOrigins []string `yaml:"allowed_origins"` // Use explicit domains in production
	AllowedMethods []string `yaml:"allowed_methods"`
	AllowedHeaders []string `yaml:"allowed_headers"`
}

// ---------------------------------------------------------------------------
// Resiliency
// ---------------------------------------------------------------------------

// ResiliencyConfig protects the server from overload and cascading failures.
type ResiliencyConfig struct {
	RateLimitRequests int                  `yaml:"rate_limit_requests"` // Max requests per second (0 = unlimited)
	CircuitBreaker    CircuitBreakerConfig `yaml:"circuit_breaker"`
}

// CircuitBreakerConfig guards AI/external calls. After FailureThreshold
// consecutive failures the breaker opens and all calls short-circuit until
// ResetTimeout elapses.
type CircuitBreakerConfig struct {
	Enabled          bool   `yaml:"enabled"`
	FailureThreshold int    `yaml:"failure_threshold"` // Failures before opening
	ResetTimeout     string `yaml:"reset_timeout"`     // Duration string ("30s", "1m", etc.)
}

// ---------------------------------------------------------------------------
// Baseline detection
// ---------------------------------------------------------------------------

// BaselineConfig tunes how historical pattern baselines are computed and
// compared against incoming data.
type BaselineConfig struct {
	WindowDays                int     `yaml:"window_days"`                 // Days of history to consider
	MinDays                   int     `yaml:"min_days"`                    // Min days before baseline is valid
	ComputationMode           string  `yaml:"computation_mode"`            // "daily" (cached) or "always" (per-request)
	TrendThreshold            float64 `yaml:"trend_threshold"`             // Fraction change to flag a trend (0.10 = 10%)
	HighSignificanceThreshold float64 `yaml:"high_significance_threshold"` // Fraction for high-significance (0.15 = 15%)
	StdDeviationMultiplier    float64 `yaml:"std_deviation_multiplier"`    // Multiplier for significance detection
}

// ---------------------------------------------------------------------------
// AI layers (all use Groq LLM)
// ---------------------------------------------------------------------------

// AIAnalyzerConfig toggles the AI Analyzer layer (Layer 7).
// When enabled, baseline comparison results are summarised in natural language.
type AIAnalyzerConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Provider string `yaml:"provider"` // "groq"
	APIKey   string `yaml:"api_key"`  // Env override: VELUM_AI_API_KEY
	Model    string `yaml:"model"`    // e.g. "llama-3.1-8b-instant"
}

// VocabAgentConfig toggles the Vocab Enricher layer (Layer 1).
// When enabled, unknown words in event names are classified via AI as
// status / surface / flow and stored to the vocabulary table.
type VocabAgentConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Provider string `yaml:"provider"` // "groq"
	APIKey   string `yaml:"api_key"`  // Env override: VELUM_VOCAB_AGENT_API_KEY
	Model    string `yaml:"model"`
}

// ContextAgentConfig toggles the Context Enricher layer (Layer 0).
// When enabled, unknown event properties are classified via AI as
// target / condition and stored to the property_registry table.
type ContextAgentConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Provider string `yaml:"provider"` // "groq"
	APIKey   string `yaml:"api_key"`  // Env override: VELUM_CONTEXT_AGENT_API_KEY
	Model    string `yaml:"model"`
}

// ---------------------------------------------------------------------------
// Data mapping
// ---------------------------------------------------------------------------

// DataMappingConfig enables declarative field mapping so callers can send
// events in their own schema and have Velum normalise them before processing.
type DataMappingConfig struct {
	Enabled bool                        `yaml:"enabled"`
	Mapping map[string]FieldMappingSpec `yaml:"mapping"`
}

// FieldMappingSpec defines how a single canonical field is extracted from
// a raw event. Paths are tried in order; the first match wins.
type FieldMappingSpec struct {
	Paths    []string `yaml:"paths"`              // Dot-notation fallback paths (e.g. "payload.event.action")
	Format   string   `yaml:"format,omitempty"`   // Timestamp format: "epoch_ms" | "epoch_s" | "iso8601"
	Required bool     `yaml:"required,omitempty"` // When true the request fails if all paths miss
}

// ---------------------------------------------------------------------------
// Defaults
// ---------------------------------------------------------------------------

// DefaultConfig returns a Config populated with sensible defaults.
// AI layers are disabled by default; storage must be configured by the user.
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
		Storage: StorageConfig{
			Type:          "postgres",
			RetentionDays: 90,
		},
		Security: SecurityConfig{
			Enabled:    false,
			APIKeyHash: "",
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
			Model:    "llama-3.1-8b-instant",
		},
		VocabAgent: VocabAgentConfig{
			Enabled:  false,
			Provider: "groq",
			Model:    "llama-3.1-8b-instant",
		},
		ContextAgent: ContextAgentConfig{
			Enabled:  false,
			Provider: "groq",
			Model:    "llama-3.1-8b-instant",
		},
		DataMapping: DataMappingConfig{
			Enabled: false,
		},
	}
}

// ---------------------------------------------------------------------------
// Loaders
// ---------------------------------------------------------------------------

// Load reads configuration from the first config file found, then applies
// environment variable overrides on top. Falls back to DefaultConfig when
// no file is present.
//
// File search order: config.yaml, config.yml, /etc/velum/config.yaml
func Load() *Config {
	// Load .env file if present (before anything reads env vars).
	if err := godotenv.Load(); err != nil {
		slog.Debug("no .env file found, relying on shell environment")
	}

	cfg := DefaultConfig()

	paths := []string{
		"config.yaml",
		"config.yml",
		"/etc/velum/config.yaml",
	}

	var loaded bool
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			slog.Warn("failed to parse config file", "path", path, "error", err)
			continue
		}
		slog.Info("loaded configuration", "path", path)
		loaded = true
		break
	}

	if !loaded {
		slog.Info("no config file found, using defaults")
	}

	// Environment variables override file values.
	applyEnvOverrides(cfg)

	return cfg
}

// LoadFromFile loads configuration from a specific file path.
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

// applyEnvOverrides sets config fields from VELUM_* environment variables.
func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("VELUM_PORT"); v != "" {
		cfg.Server.Port = v
	}
	if v := os.Getenv("VELUM_ENV"); v != "" {
		cfg.Server.Environment = v
	}
	if v := os.Getenv("VELUM_DB_HOST"); v != "" {
		cfg.Storage.Postgres.Host = v
	}
	if v := os.Getenv("VELUM_DB_PASSWORD"); v != "" {
		cfg.Storage.Postgres.Password = v
	}
	if v := os.Getenv("VELUM_AI_API_KEY"); v != "" {
		cfg.AIAnalyzer.APIKey = v
	}
	if v := os.Getenv("VELUM_VOCAB_AGENT_API_KEY"); v != "" {
		cfg.VocabAgent.APIKey = v
	}
	if v := os.Getenv("VELUM_CONTEXT_AGENT_API_KEY"); v != "" {
		cfg.ContextAgent.APIKey = v
	}
	if v := os.Getenv("VELUM_API_KEY_HASH"); v != "" {
		cfg.Security.APIKeyHash = v
	}
}
