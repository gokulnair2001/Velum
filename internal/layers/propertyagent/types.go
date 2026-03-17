package propertyagent

import "time"

// PropertyRole represents the role a property plays in behavioral analysis.
type PropertyRole string

const (
	RoleDimension PropertyRole = "dimension"
	RoleTarget    PropertyRole = "target"
	RoleCondition PropertyRole = "condition"
	RoleMeasure   PropertyRole = "measure"
)

// PropertyEntry represents a classified property in the registry.
type PropertyEntry struct {
	KeyName     string       `json:"key_name"`
	Role        PropertyRole `json:"role"`
	Label       string       `json:"label,omitempty"`
	SampleValue string       `json:"sample_value,omitempty"`
	Source      string       `json:"source"` // "builtin", "inferred", "ai", "manual"
	CreatedAt   string       `json:"created_at,omitempty"`
}

// ClassificationResult holds the AI classification output.
// The AI classifies unknown string properties into target or condition.
type ClassificationResult struct {
	Target    []string `json:"target"`
	Condition []string `json:"condition"`
}

// Config holds configuration for the Property Agent.
type Config struct {
	Enabled        bool
	Provider       string
	BaseURL        string
	APIKey         string
	Model          string
	Debug          bool
	CircuitBreaker CircuitBreakerConfig
}

// CircuitBreakerConfig holds circuit breaker configuration.
type CircuitBreakerConfig struct {
	Enabled          bool
	FailureThreshold int
	ResetTimeout     time.Duration
}

// DefaultConfig returns the default Property Agent configuration.
func DefaultConfig() *Config {
	return &Config{
		Enabled: false,
		BaseURL: "",
		APIKey:  "",
		Model:   "llama-3.1-8b-instant",
		CircuitBreaker: CircuitBreakerConfig{
			Enabled:          true,
			FailureThreshold: 5,
			ResetTimeout:     30 * time.Second,
		},
	}
}

// UnknownProperty represents a property key + sample value awaiting AI classification.
type UnknownProperty struct {
	Key         string `json:"key"`
	SampleValue string `json:"sample_value"`
}

// GroqRequest represents the request body for Groq API.
type GroqRequest struct {
	Model       string        `json:"model"`
	Messages    []GroqMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
}

// GroqMessage represents a message in the Groq API format.
type GroqMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// GroqResponse represents the response from Groq API.
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

// GroqError represents an error response from Groq API.
type GroqError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}
