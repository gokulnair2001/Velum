package vocabagent

import "time"

// VocabAgentResult contains the output of the vocab agent layer
type VocabAgentResult struct {
	// Original uncategorized words that were processed
	UncategorizedWords []string `json:"uncategorized_words"`

	// AI-determined classifications grouped by category
	Classified *VocabData `json:"classified"`

	// Whether the vocab agent was enabled and used
	AgentEnabled bool `json:"agent_enabled"`

	// Error message if classification failed
	Error string `json:"error,omitempty"`
}

// Config holds configuration for the vocab agent layer
type Config struct {
	// Enabled determines if vocab agent is active
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

// CircuitBreakerConfig holds configuration for the circuit breaker
type CircuitBreakerConfig struct {
	// Enabled determines if the circuit breaker is active
	Enabled bool

	// FailureThreshold is the number of failures before opening the circuit
	FailureThreshold int

	// ResetTimeout is how long to wait before attempting to close the circuit
	ResetTimeout time.Duration
}

// DefaultConfig returns the default vocab agent configuration
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
