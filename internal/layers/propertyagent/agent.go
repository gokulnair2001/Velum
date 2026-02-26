package propertyagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	groqAPIEndpoint      = "https://api.groq.com/openai/v1/chat/completions"
	propertySystemPrompt = `You are a property classification agent for a product analytics system.

Your task is to classify unknown event property keys into exactly one of two roles:

1. target
Properties that describe WHAT the action is about. These are domain-specific object references.

Examples of target properties:
- plan_name (what plan: "premium", "free")
- product_id (what product: "SKU-123")
- page_name (what page: "pricing")
- feature_name (what feature: "dark_mode")
- item_type (what item: "video", "article")
- document_id (what document)
- campaign_name (what campaign)
- subscription_tier (what tier)

2. condition
Properties that describe the CIRCUMSTANCES or CONTEXT under which the action occurred.
These are causal, explanatory, or environmental factors.

Examples of condition properties:
- error_code (why it failed: "card_declined")
- ab_variant (which experiment: "B")
- referral_source (how they got here: "email")
- payment_method (how they paid: "stripe")
- is_first_time (was it their first time: true)
- retry_count (how many retries: 3)
- trigger_type (what triggered it: "auto", "manual")
- exit_reason (why they left: "timeout")
- discount_code (what discount was applied)

Critical Rules:
- Each property key MUST be classified into exactly ONE category: target or condition.
- Use the original property key exactly as provided (no modification).
- If a sample value is provided, use it to inform your classification.
- Properties describing the object/entity of the action → target.
- Properties describing circumstances/why/how of the action → condition.
- Do NOT explain reasoning.
- Respond with valid JSON only.

Output Format (MANDATORY):
{
  "target": ["key1", "key2"],
  "condition": ["key3", "key4"]
}

- Keys MUST always be present.
- Arrays may be empty.`
)

// PropertyAgent performs AI-powered classification of unknown property keys
// into target or condition roles.
type PropertyAgent struct {
	config         *Config
	httpClient     *http.Client
	circuitBreaker *CircuitBreaker
}

// NewAgent creates a new PropertyAgent with default config (disabled).
func NewAgent() *PropertyAgent {
	cfg := DefaultConfig()
	return &PropertyAgent{
		config: cfg,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		circuitBreaker: NewCircuitBreaker(cfg.CircuitBreaker, false),
	}
}

// NewAgentWithConfig creates a PropertyAgent with custom config.
func NewAgentWithConfig(config *Config) *PropertyAgent {
	return &PropertyAgent{
		config: config,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		circuitBreaker: NewCircuitBreaker(config.CircuitBreaker, config.Debug),
	}
}

// ClassifyProperties classifies a batch of unknown properties into target or condition.
// Only string-valued (non-numeric, non-dimension) properties reach this method.
func (a *PropertyAgent) ClassifyProperties(properties []UnknownProperty) (*ClassificationResult, error) {
	return a.ClassifyPropertiesCtx(context.Background(), properties)
}

// ClassifyPropertiesCtx is like ClassifyProperties but accepts a context.
func (a *PropertyAgent) ClassifyPropertiesCtx(ctx context.Context, properties []UnknownProperty) (*ClassificationResult, error) {
	emptyResult := &ClassificationResult{Target: []string{}, Condition: []string{}}

	if !a.config.Enabled || a.config.APIKey == "" {
		return emptyResult, nil
	}

	if len(properties) == 0 {
		return emptyResult, nil
	}

	// Check circuit breaker
	if err := a.circuitBreaker.Allow(); err != nil {
		if a.config.Debug {
			slog.Debug("circuit breaker is open, skipping classification", "layer", "property_agent")
		}
		return emptyResult, fmt.Errorf("circuit breaker open")
	}

	result, err := a.callAI(ctx, properties)
	if err != nil {
		a.circuitBreaker.RecordFailure()
		if a.config.Debug {
			slog.Debug("classification failed", "layer", "property_agent", "error", err)
		}
		return emptyResult, err
	}

	a.circuitBreaker.RecordSuccess()
	return result, nil
}

// callAI makes the actual API call to classify properties.
func (a *PropertyAgent) callAI(ctx context.Context, properties []UnknownProperty) (*ClassificationResult, error) {
	if a.config.Debug {
		slog.Debug("calling AI to classify properties", "layer", "property_agent", "count", len(properties))
	}

	// Build user message with property keys and sample values
	var lines []string
	for _, p := range properties {
		if p.SampleValue != "" {
			lines = append(lines, fmt.Sprintf("- %s (sample: %s)", p.Key, p.SampleValue))
		} else {
			lines = append(lines, fmt.Sprintf("- %s", p.Key))
		}
	}

	userMessage := fmt.Sprintf("Classify these event property keys into target or condition:\n%s", strings.Join(lines, "\n"))

	// Build request
	request := GroqRequest{
		Model: a.config.Model,
		Messages: []GroqMessage{
			{Role: "system", Content: propertySystemPrompt},
			{Role: "user", Content: userMessage},
		},
		Temperature: 0.1,
		MaxTokens:   1024,
	}

	requestBody, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", groqAPIEndpoint, bytes.NewReader(requestBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.config.APIKey)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if a.config.Debug {
		slog.Debug("API response received", "layer", "property_agent", "status", resp.StatusCode)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var groqResp GroqResponse
	if err := json.Unmarshal(body, &groqResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if groqResp.Error != nil {
		return nil, fmt.Errorf("API error: %s", groqResp.Error.Message)
	}

	if len(groqResp.Choices) == 0 {
		return nil, fmt.Errorf("no response from AI")
	}

	content := groqResp.Choices[0].Message.Content
	content = strings.TrimSpace(content)

	if a.config.Debug {
		slog.Debug("AI response content", "layer", "property_agent", "content", content)
	}

	// Parse the JSON response
	var classified ClassificationResult
	if err := json.Unmarshal([]byte(content), &classified); err != nil {
		// Try to extract JSON from markdown code blocks
		content = extractJSON(content)
		if err := json.Unmarshal([]byte(content), &classified); err != nil {
			return nil, fmt.Errorf("failed to parse AI response as JSON: %w", err)
		}
	}

	// Ensure arrays are initialized
	if classified.Target == nil {
		classified.Target = []string{}
	}
	if classified.Condition == nil {
		classified.Condition = []string{}
	}

	return &classified, nil
}

// extractJSON attempts to extract JSON from a string wrapped in markdown code blocks.
func extractJSON(s string) string {
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

// GetConfig returns the current configuration.
func (a *PropertyAgent) GetConfig() *Config {
	return a.config
}

// CircuitBreakerStats returns the current circuit breaker statistics.
func (a *PropertyAgent) CircuitBreakerStats() map[string]interface{} {
	return a.circuitBreaker.Stats()
}
