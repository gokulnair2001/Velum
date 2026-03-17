package vocabagent

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

	"github.com/velum/internal/config"
)

const (
	systemPrompt = `You are a vocabulary classification agent for a product analytics system.

You are a vocabulary classification assistant for a product analytics system.

Your task is to classify unknown words from user event data into exactly one of the following categories:

1. status

Describes what happened, the nature of an interaction, or the outcome.

Represents:
	•	Actions
	•	State transitions
	•	Interaction types
	•	Outcomes

Examples:
view, click, submit, success, error, exit, retry, open, close, scroll

2. surface

Describes a UI component, screen, feature area, or product location.

Represents:
	•	Where an interaction occurs
	•	UI context
	•	Feature/module names

Examples:
modal, button, page, sidebar, search, settings, document, comment, share

3. flow

Describes a higher-level user goal, journey, or intent-driven activity.

Represents:
	•	Why interactions occur
	•	Behavioral intent
	•	User journeys

Examples:
authentication, registration, purchase, discovery, configuration, editing, sharing

Critical Rules
	•	Each word MUST be classified into exactly ONE category.
	•	A word MUST NOT appear in more than one category.
	•	Use the original word exactly as provided (lowercase, no modification).
	•	Words describing actions, transitions, or outcomes MUST be classified as status.
	•	Words describing UI areas, screens, features, or components MUST be classified as surface.
	•	Words describing user goals, journeys, or intent MUST be classified as flow.
	•	Do NOT invent meanings beyond common product analytics terminology.
	•	If a word cannot be meaningfully classified, omit it.
	•	Do NOT explain reasoning.
	•	Respond with valid JSON only.

Handling Overlapping Words (IMPORTANT)

Some words may reasonably represent both a UI area and a user goal (for example: checkout, search, settings, upload, share).

In such cases:
	•	Classify the word based on its MOST COMMON usage in product analytics systems.
	•	Prefer surface when the word primarily represents a feature, screen, or product area.
	•	Prefer flow only when the word clearly represents a broader user intent beyond a specific UI location.

Examples:

checkout → surface
search → surface
settings → surface
upload → surface
share → surface

authentication → flow
purchase → flow

Output Format (MANDATORY)

You MUST respond with valid JSON ONLY in this exact format:

{
“status”: [“word1”, “word2”],
“surface”: [“word3”, “word4”],
“flow”: [“word5”, “word6”]
}
	•	Keys MUST always be present.
	•	Arrays may be empty.`
)

// VocabAgent performs AI-powered classification of uncategorized words
type VocabAgent struct {
	config         *Config
	httpClient     *http.Client
	circuitBreaker *CircuitBreaker
}

// New creates a new VocabAgent with default config (disabled)
func New() *VocabAgent {
	cfg := DefaultConfig()
	return &VocabAgent{
		config: cfg,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		circuitBreaker: NewCircuitBreaker(cfg.CircuitBreaker, false),
	}
}

// NewWithConfig creates a VocabAgent with custom config
func NewWithConfig(config *Config) *VocabAgent {
	return &VocabAgent{
		config: config,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		circuitBreaker: NewCircuitBreaker(config.CircuitBreaker, config.Debug),
	}
}

// Name returns the layer identifier
func (v *VocabAgent) Name() string {
	return "vocab_agent"
}

// apiEndpoint returns the configured API endpoint, resolving from provider if base_url is not set.
func (v *VocabAgent) apiEndpoint() string {
	return config.ResolveProviderURL(v.config.Provider, v.config.BaseURL)
}

// Process implements the Layer interface
func (v *VocabAgent) Process(input interface{}) (interface{}, error) {
	// If vocab agent is disabled, pass through the input
	if !v.config.Enabled || v.config.APIKey == "" {
		if v.config.Debug {
			slog.Debug("vocab agent disabled, passing through", "layer", "vocab_agent")
		}
		return input, nil
	}

	// Extract uncategorized words from input
	uncategorizedWords := v.extractUncategorizedWords(input)

	if len(uncategorizedWords) == 0 {
		if v.config.Debug {
			slog.Debug("no uncategorized words found, passing through", "layer", "vocab_agent")
		}
		return input, nil
	}

	if v.config.Debug {
		slog.Debug("found uncategorized words", "layer", "vocab_agent", "count", len(uncategorizedWords), "words", uncategorizedWords)
	}

	// Check circuit breaker before making request
	if err := v.circuitBreaker.Allow(); err != nil {
		if v.config.Debug {
			slog.Debug("circuit breaker is open, skipping classification", "layer", "vocab_agent")
		}
		return &VocabAgentResult{
			UncategorizedWords: uncategorizedWords,
			Classified:         &VocabData{Status: []string{}, Surface: []string{}, Flow: []string{}},
			AgentEnabled:       true,
			Error:              "circuit breaker open",
		}, nil
	}

	// Call AI to classify words
	classified, err := v.classifyWords(context.Background(), uncategorizedWords)
	if err != nil {
		v.circuitBreaker.RecordFailure()
		if v.config.Debug {
			slog.Debug("classification failed", "layer", "vocab_agent", "error", err)
		}
		return &VocabAgentResult{
			UncategorizedWords: uncategorizedWords,
			Classified:         &VocabData{Status: []string{}, Surface: []string{}, Flow: []string{}},
			AgentEnabled:       true,
			Error:              err.Error(),
		}, nil
	}

	v.circuitBreaker.RecordSuccess()

	totalClassified := len(classified.Status) + len(classified.Surface) + len(classified.Flow)
	if v.config.Debug {
		slog.Debug("successfully classified words", "layer", "vocab_agent", "count", totalClassified)
	}

	return &VocabAgentResult{
		UncategorizedWords: uncategorizedWords,
		Classified:         classified,
		AgentEnabled:       true,
	}, nil
}

// ClassifyWords classifies a list of uncategorized words (public method for direct use)
func (v *VocabAgent) ClassifyWords(words []string) (*VocabAgentResult, error) {
	return v.ClassifyWordsCtx(context.Background(), words)
}

// ClassifyWordsCtx is like ClassifyWords but accepts a context for cancellation.
func (v *VocabAgent) ClassifyWordsCtx(ctx context.Context, words []string) (*VocabAgentResult, error) {
	emptyResult := &VocabData{Status: []string{}, Surface: []string{}, Flow: []string{}}

	if !v.config.Enabled || v.config.APIKey == "" {
		return &VocabAgentResult{
			UncategorizedWords: words,
			Classified:         emptyResult,
			AgentEnabled:       false,
		}, nil
	}

	if len(words) == 0 {
		return &VocabAgentResult{
			UncategorizedWords: words,
			Classified:         emptyResult,
			AgentEnabled:       true,
		}, nil
	}

	// Check circuit breaker
	if err := v.circuitBreaker.Allow(); err != nil {
		return &VocabAgentResult{
			UncategorizedWords: words,
			Classified:         emptyResult,
			AgentEnabled:       true,
			Error:              "circuit breaker open",
		}, nil
	}

	classified, err := v.classifyWords(ctx, words)
	if err != nil {
		v.circuitBreaker.RecordFailure()
		return &VocabAgentResult{
			UncategorizedWords: words,
			Classified:         emptyResult,
			AgentEnabled:       true,
			Error:              err.Error(),
		}, nil
	}

	v.circuitBreaker.RecordSuccess()

	return &VocabAgentResult{
		UncategorizedWords: words,
		Classified:         classified,
		AgentEnabled:       true,
	}, nil
}

// extractUncategorizedWords extracts uncategorized words from various input types
func (v *VocabAgent) extractUncategorizedWords(input interface{}) []string {
	words := make([]string, 0)
	seen := make(map[string]bool)

	switch val := input.(type) {
	case []string:
		// Direct list of words
		for _, w := range val {
			lower := strings.ToLower(w)
			if !seen[lower] {
				words = append(words, lower)
				seen[lower] = true
			}
		}

	case map[string]interface{}:
		// Look for uncategorized field
		if uncategorized, ok := val["uncategorized"]; ok {
			words = v.extractWordsFromInterface(uncategorized, seen)
		}
		// Also check normalized.uncategorized
		if normalized, ok := val["normalized"].(map[string]interface{}); ok {
			if uncategorized, ok := normalized["uncategorized"]; ok {
				words = append(words, v.extractWordsFromInterface(uncategorized, seen)...)
			}
		}

	case []map[string]interface{}:
		// Multiple events
		for _, event := range val {
			if uncategorized, ok := event["uncategorized"]; ok {
				words = append(words, v.extractWordsFromInterface(uncategorized, seen)...)
			}
			if normalized, ok := event["normalized"].(map[string]interface{}); ok {
				if uncategorized, ok := normalized["uncategorized"]; ok {
					words = append(words, v.extractWordsFromInterface(uncategorized, seen)...)
				}
			}
		}
	}

	return words
}

// extractWordsFromInterface extracts words from an interface value
func (v *VocabAgent) extractWordsFromInterface(val interface{}, seen map[string]bool) []string {
	words := make([]string, 0)

	switch w := val.(type) {
	case []string:
		for _, word := range w {
			lower := strings.ToLower(word)
			if !seen[lower] {
				words = append(words, lower)
				seen[lower] = true
			}
		}
	case []interface{}:
		for _, item := range w {
			if str, ok := item.(string); ok {
				lower := strings.ToLower(str)
				if !seen[lower] {
					words = append(words, lower)
					seen[lower] = true
				}
			}
		}
	}

	return words
}

// classifyWords calls the AI API to classify words
func (v *VocabAgent) classifyWords(ctx context.Context, words []string) (*VocabData, error) {
	if v.config.Debug {
		slog.Debug("calling AI to classify words", "layer", "vocab_agent", "words", words)
	}

	// Build user message with the words to classify
	wordsJSON, err := json.Marshal(words)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal words: %w", err)
	}

	userMessage := fmt.Sprintf("Classify these unknown words from user analytics events: %s", string(wordsJSON))

	// Build request
	request := GroqRequest{
		Model: v.config.Model,
		Messages: []GroqMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userMessage},
		},
		Temperature: 0.1, // Low temperature for consistent classifications
		MaxTokens:   1024,
	}

	requestBody, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", v.apiEndpoint(), bytes.NewReader(requestBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+v.config.APIKey)

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if v.config.Debug {
		slog.Debug("API response received", "layer", "vocab_agent", "status", resp.StatusCode)
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

	// Extract and parse the AI's classification response
	content := groqResp.Choices[0].Message.Content
	content = strings.TrimSpace(content)

	if v.config.Debug {
		slog.Debug("AI response content", "layer", "vocab_agent", "content", content)
	}

	// Parse the JSON response from the AI
	var classified VocabData
	if err := json.Unmarshal([]byte(content), &classified); err != nil {
		// Try to extract JSON from the response if it's wrapped in markdown
		content = extractJSON(content)
		if err := json.Unmarshal([]byte(content), &classified); err != nil {
			return nil, fmt.Errorf("failed to parse AI response as JSON: %w", err)
		}
	}

	// Ensure arrays are initialized (not nil)
	if classified.Status == nil {
		classified.Status = []string{}
	}
	if classified.Surface == nil {
		classified.Surface = []string{}
	}
	if classified.Flow == nil {
		classified.Flow = []string{}
	}

	return &classified, nil
}

// extractJSON attempts to extract JSON from a string that might be wrapped in markdown code blocks
func extractJSON(s string) string {
	// Remove markdown code block markers
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

// GetConfig returns the current configuration
func (v *VocabAgent) GetConfig() *Config {
	return v.config
}

// CircuitBreakerStats returns the current circuit breaker statistics
func (v *VocabAgent) CircuitBreakerStats() map[string]interface{} {
	return v.circuitBreaker.Stats()
}
