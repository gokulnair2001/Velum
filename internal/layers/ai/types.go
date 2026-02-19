package ai

import (
	"time"

	"github.com/velum/internal/layers/baseline"
)

// Config holds configuration for the AI layer
type Config struct {
	// Enabled determines if AI analysis is active
	Enabled bool

	// APIKey is the Groq API key
	APIKey string

	// Model is the LLM model to use
	Model string

	// Debug enables verbose logging
	Debug bool

	// CircuitBreaker configuration
	CircuitBreaker CircuitBreakerConfig
}

// DefaultConfig returns the default AI configuration
func DefaultConfig() *Config {
	return &Config{
		Enabled: false,
		APIKey:  "",
		Model:   "llama-3.1-8b-instant",
		CircuitBreaker: CircuitBreakerConfig{
			Enabled:          true,
			FailureThreshold: 5,
			ResetTimeout:     30 * time.Second,
		},
	}
}

// AnalysisResponse represents the structured AI analysis output
type AnalysisResponse struct {
	Summary        string   `json:"summary"`
	Details        []string `json:"details"`
	Hypotheses     []string `json:"hypotheses"`
	ConfidenceNote string   `json:"confidence_note"`
}

// AIResult contains the output of the AI layer
type AIResult struct {
	// Change results from baseline comparison
	ChangeResults []*baseline.ChangeResult `json:"change_results"`

	// DetectedPatterns carries pattern evidence (severity, confidence, etc.)
	DetectedPatterns interface{} `json:"detected_patterns,omitempty"`

	// AnalyzedFlows carries per-flow context (dimensions, conditions, targets)
	AnalyzedFlows interface{} `json:"analyzed_flows,omitempty"`

	// AI analysis (only populated when AI is enabled)
	AIAnalysis *AnalysisResponse `json:"ai_analysis,omitempty"`
	AIEnabled  bool              `json:"ai_enabled"`
}

// GroqRequest represents the request body for Groq API
type GroqRequest struct {
	Model       string        `json:"model"`
	Messages    []GroqMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
}

// GroqMessage represents a message in the Groq API format
type GroqMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// GroqResponse represents the response from Groq API
type GroqResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Error *GroqError `json:"error,omitempty"`
}

// GroqError represents an error response from Groq API
type GroqError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}
