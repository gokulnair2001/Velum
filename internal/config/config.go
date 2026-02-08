package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config holds application configuration
type Config struct {
	Server     ServerConfig     `yaml:"server"`
	CORS       CORSConfig       `yaml:"cors"`
	Resiliency ResiliencyConfig `yaml:"resiliency"`
	Storage    StorageConfig    `yaml:"storage"`
	Baseline   BaselineConfig   `yaml:"baseline"`
	AI         AIConfig         `yaml:"ai"`
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
	RateLimitRequests int                   `yaml:"rate_limit_requests"` // Requests per second
	CircuitBreaker    CircuitBreakerConfig  `yaml:"circuit_breaker"`
}

// CircuitBreakerConfig holds circuit breaker configuration for AI layer
type CircuitBreakerConfig struct {
	Enabled          bool   `yaml:"enabled"`
	FailureThreshold int    `yaml:"failure_threshold"` // Number of failures before opening
	ResetTimeout     string `yaml:"reset_timeout"`     // Time to wait before trying again
}

// StorageConfig holds storage-related configuration
type StorageConfig struct {
	RetentionDays int `yaml:"retention_days"`
}

// BaselineConfig holds baseline detection configuration
type BaselineConfig struct {
	WindowDays              int     `yaml:"window_days"`
	MinDays                 int     `yaml:"min_days"`
	ComputationMode         string  `yaml:"computation_mode"` // "daily" or "always"
	TrendThreshold          float64 `yaml:"trend_threshold"`
	HighSignificanceThreshold float64 `yaml:"high_significance_threshold"`
	StdDeviationMultiplier  float64 `yaml:"std_deviation_multiplier"`
}

// AIConfig holds AI layer configuration
type AIConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Provider string `yaml:"provider"`
	APIKey   string `yaml:"api_key"`
	Model    string `yaml:"model"`
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
			RetentionDays: 90,
		},
		Baseline: BaselineConfig{
			WindowDays:              28,
			MinDays:                 7,
			ComputationMode:         "daily",
			TrendThreshold:          0.10,
			HighSignificanceThreshold: 0.15,
			StdDeviationMultiplier:  2.0,
		},
		AI: AIConfig{
			Enabled:  true,
			Provider: "groq",
			APIKey:   "",
			Model:    "llama-3.1-8b-instant",
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
		cfg.AI.APIKey = aiKey
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
