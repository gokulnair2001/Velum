package vocabagent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	groqAPIEndpoint = "https://api.groq.com/openai/v1/chat/completions"
	systemPrompt    = `You are a vocabulary classification agent for a product analytics system.

You are a vocabulary classification assistant for a product analytics system.

Your task is to classify unknown words from user event data into exactly one of the following categories:

1. status  
   - Describes what happened or the outcome of an action  
   - Examples: view, click, submit, success, error, exit, retry, open, close, scroll  

2. surface  
   - Describes a UI component, screen, or location where the action occurred  
   - Examples: page, modal, button, card, sidebar, search, settings, document, comment, share  

3. flow  
   - Describes a user goal, journey, or intent-driven activity  
   - Examples: authentication, registration, checkout, purchase, navigation, configuration, sharing, editing  

Rules:
- Each word MUST be placed into exactly ONE category.
- A word MUST NOT appear in more than one category.
- Use the original word exactly as provided (lowercase, no modification).
- If a word represents an action or verb (e.g., open, click, scroll), it MUST be classified as status, never as flow.
- A flow must represent a higher-level user intent, not a UI action or event name.
- If a word cannot be meaningfully classified into any category, omit it entirely.
- Do NOT invent new categories.
- Do NOT explain your reasoning.
- Do NOT include any words that are already well-known system keywords unless they clearly fit one category.
- Be consistent with common product analytics and event-tracking terminology.

You MUST respond with valid JSON ONLY in the following exact format:

{
  "status": ["word1", "word2"],
  "surface": ["word3", "word4"],
  "flow": ["word5", "word6"]
}

Each array may be empty, but the keys MUST always be present.`
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

// Process implements the Layer interface
func (v *VocabAgent) Process(input interface{}) (interface{}, error) {
	// If vocab agent is disabled, pass through the input
	if !v.config.Enabled || v.config.APIKey == "" {
		if v.config.Debug {
			fmt.Println("[DEBUG] [VocabAgent] Vocab agent disabled, passing through data")
		}
		return input, nil
	}

	// Extract uncategorized words from input
	uncategorizedWords := v.extractUncategorizedWords(input)

	if len(uncategorizedWords) == 0 {
		if v.config.Debug {
			fmt.Println("[DEBUG] [VocabAgent] No uncategorized words found, passing through")
		}
		return input, nil
	}

	if v.config.Debug {
		fmt.Printf("[DEBUG] [VocabAgent] Found %d uncategorized words: %v\n", len(uncategorizedWords), uncategorizedWords)
	}

	// Check circuit breaker before making request
	if err := v.circuitBreaker.Allow(); err != nil {
		if v.config.Debug {
			fmt.Println("[DEBUG] [VocabAgent] Circuit breaker is open, skipping classification")
		}
		return &VocabAgentResult{
			UncategorizedWords: uncategorizedWords,
			Classified:         &VocabData{Status: []string{}, Surface: []string{}, Flow: []string{}},
			AgentEnabled:       true,
			Error:              "circuit breaker open",
		}, nil
	}

	// Call AI to classify words
	classified, err := v.classifyWords(uncategorizedWords)
	if err != nil {
		v.circuitBreaker.RecordFailure()
		if v.config.Debug {
			fmt.Printf("[DEBUG] [VocabAgent] Classification failed: %v\n", err)
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
		fmt.Printf("[DEBUG] [VocabAgent] Successfully classified %d words\n", totalClassified)
	}

	return &VocabAgentResult{
		UncategorizedWords: uncategorizedWords,
		Classified:         classified,
		AgentEnabled:       true,
	}, nil
}

// ClassifyWords classifies a list of uncategorized words (public method for direct use)
func (v *VocabAgent) ClassifyWords(words []string) (*VocabAgentResult, error) {
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

	classified, err := v.classifyWords(words)
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
func (v *VocabAgent) classifyWords(words []string) (*VocabData, error) {
	if v.config.Debug {
		fmt.Printf("[DEBUG] [VocabAgent] Calling AI to classify: %v\n", words)
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
	req, err := http.NewRequest("POST", groqAPIEndpoint, bytes.NewReader(requestBody))
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
		fmt.Printf("[DEBUG] [VocabAgent] API response status: %d\n", resp.StatusCode)
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
		fmt.Printf("[DEBUG] [VocabAgent] AI response content: %s\n", content)
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
