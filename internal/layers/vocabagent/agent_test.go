package vocabagent

import (
	"testing"
	"time"
)

func TestVocabAgentName(t *testing.T) {
	agent := New()
	if agent.Name() != "vocab_agent" {
		t.Errorf("Expected name 'vocab_agent', got '%s'", agent.Name())
	}
}

func TestVocabAgentDisabled(t *testing.T) {
	// Test with vocab agent disabled (default)
	agent := New()

	words := []string{"customword", "anotherterm"}

	result, err := agent.Process(words)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// When disabled, should pass through input unchanged
	resultWords, ok := result.([]string)
	if !ok {
		t.Fatal("Expected []string type when disabled")
	}

	if len(resultWords) != 2 {
		t.Errorf("Expected 2 words, got %d", len(resultWords))
	}
}

func TestVocabAgentWithConfig(t *testing.T) {
	config := &Config{
		Enabled: true,
		APIKey:  "", // Empty key should still disable actual API calls
		Model:   "llama-3.1-8b-instant",
	}

	agent := NewWithConfig(config)

	if agent.config.Enabled != true {
		t.Error("Expected Enabled to be true")
	}

	if agent.config.Model != "llama-3.1-8b-instant" {
		t.Errorf("Expected model 'llama-3.1-8b-instant', got '%s'", agent.config.Model)
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Enabled {
		t.Error("Expected Enabled to be false by default")
	}

	if config.APIKey != "" {
		t.Error("Expected empty APIKey by default")
	}

	if config.Model != "llama-3.1-8b-instant" {
		t.Errorf("Expected model 'llama-3.1-8b-instant', got '%s'", config.Model)
	}

	if !config.CircuitBreaker.Enabled {
		t.Error("Expected circuit breaker to be enabled by default")
	}

	if config.CircuitBreaker.FailureThreshold != 5 {
		t.Errorf("Expected failure threshold 5, got %d", config.CircuitBreaker.FailureThreshold)
	}

	if config.CircuitBreaker.ResetTimeout != 30*time.Second {
		t.Errorf("Expected reset timeout 30s, got %v", config.CircuitBreaker.ResetTimeout)
	}
}

func TestClassifyWordsDirectly(t *testing.T) {
	// Test ClassifyWords method when disabled
	agent := New()

	result, err := agent.ClassifyWords([]string{"testword"})
	if err != nil {
		t.Fatalf("ClassifyWords failed: %v", err)
	}

	if result.AgentEnabled {
		t.Error("Expected AgentEnabled to be false when disabled")
	}

	totalClassified := len(result.Classified.Status) + len(result.Classified.Surface) + len(result.Classified.Flow)
	if totalClassified != 0 {
		t.Errorf("Expected 0 classifications when disabled, got %d", totalClassified)
	}
}

func TestClassifyWordsEmpty(t *testing.T) {
	config := &Config{
		Enabled: true,
		APIKey:  "test-key",
		Model:   "llama-3.1-8b-instant",
	}
	agent := NewWithConfig(config)

	result, err := agent.ClassifyWords([]string{})
	if err != nil {
		t.Fatalf("ClassifyWords failed: %v", err)
	}

	if !result.AgentEnabled {
		t.Error("Expected AgentEnabled to be true")
	}

	if len(result.UncategorizedWords) != 0 {
		t.Errorf("Expected 0 uncategorized words, got %d", len(result.UncategorizedWords))
	}
}

func TestExtractUncategorizedWords(t *testing.T) {
	agent := New()

	tests := []struct {
		name     string
		input    interface{}
		expected int
	}{
		{
			name:     "string slice",
			input:    []string{"word1", "word2", "word3"},
			expected: 3,
		},
		{
			name:     "string slice with duplicates",
			input:    []string{"word1", "Word1", "WORD1"},
			expected: 1, // Should deduplicate (case-insensitive)
		},
		{
			name: "map with uncategorized",
			input: map[string]interface{}{
				"uncategorized": []interface{}{"term1", "term2"},
			},
			expected: 2,
		},
		{
			name: "map with normalized.uncategorized",
			input: map[string]interface{}{
				"normalized": map[string]interface{}{
					"uncategorized": []interface{}{"termA", "termB"},
				},
			},
			expected: 2,
		},
		{
			name:     "empty input",
			input:    []string{},
			expected: 0,
		},
		{
			name:     "unsupported type",
			input:    12345,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			words := agent.extractUncategorizedWords(tt.input)
			if len(words) != tt.expected {
				t.Errorf("Expected %d words, got %d: %v", tt.expected, len(words), words)
			}
		})
	}
}

func TestCircuitBreakerStats(t *testing.T) {
	agent := New()

	stats := agent.CircuitBreakerStats()

	if stats["state"] != "closed" {
		t.Errorf("Expected state 'closed', got '%s'", stats["state"])
	}

	if stats["failure_count"] != 0 {
		t.Errorf("Expected failure_count 0, got %v", stats["failure_count"])
	}
}

func TestCircuitBreakerIntegration(t *testing.T) {
	config := &Config{
		Enabled: true,
		APIKey:  "test-api-key",
		Model:   "llama-3.1-8b-instant",
		Debug:   false,
		CircuitBreaker: CircuitBreakerConfig{
			Enabled:          true,
			FailureThreshold: 2,
			ResetTimeout:     100 * time.Millisecond,
		},
	}

	agent := NewWithConfig(config)

	// Initially circuit should be closed
	if agent.circuitBreaker.State() != CircuitClosed {
		t.Error("Expected circuit breaker to be closed initially")
	}

	// Record failures to open the circuit
	agent.circuitBreaker.RecordFailure()
	agent.circuitBreaker.RecordFailure()

	// Circuit should be open now
	if agent.circuitBreaker.State() != CircuitOpen {
		t.Error("Expected circuit breaker to be open after threshold failures")
	}

	// Wait for reset timeout
	time.Sleep(150 * time.Millisecond)

	// Allow should transition to half-open
	err := agent.circuitBreaker.Allow()
	if err != nil {
		t.Error("Expected Allow() to succeed after reset timeout")
	}

	if agent.circuitBreaker.State() != CircuitHalfOpen {
		t.Error("Expected circuit breaker to be half-open after timeout")
	}

	// Record success to close circuit
	agent.circuitBreaker.RecordSuccess()

	if agent.circuitBreaker.State() != CircuitClosed {
		t.Error("Expected circuit breaker to be closed after success in half-open state")
	}
}

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "plain json",
			input:    `{"status": [], "surface": [], "flow": []}`,
			expected: `{"status": [], "surface": [], "flow": []}`,
		},
		{
			name:     "markdown json block",
			input:    "```json\n{\"status\": [\"click\"]}\n```",
			expected: `{"status": ["click"]}`,
		},
		{
			name:     "markdown code block",
			input:    "```\n{\"flow\": [\"auth\"]}\n```",
			expected: `{"flow": ["auth"]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractJSON(tt.input)
			if result != tt.expected {
				t.Errorf("Expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestVocabDataStructure(t *testing.T) {
	data := &VocabData{
		Status:  []string{"click", "view", "success"},
		Surface: []string{"button", "modal"},
		Flow:    []string{"authentication", "checkout"},
	}

	if len(data.Status) != 3 {
		t.Errorf("Expected 3 status items, got %d", len(data.Status))
	}

	if len(data.Surface) != 2 {
		t.Errorf("Expected 2 surface items, got %d", len(data.Surface))
	}

	if len(data.Flow) != 2 {
		t.Errorf("Expected 2 flow items, got %d", len(data.Flow))
	}
}

func TestVocabAgentResult(t *testing.T) {
	result := VocabAgentResult{
		UncategorizedWords: []string{"word1", "word2", "word3"},
		Classified: &VocabData{
			Status:  []string{"word1"},
			Surface: []string{"word2"},
			Flow:    []string{"word3"},
		},
		AgentEnabled: true,
	}

	if len(result.UncategorizedWords) != 3 {
		t.Errorf("Expected 3 uncategorized words, got %d", len(result.UncategorizedWords))
	}

	totalClassified := len(result.Classified.Status) + len(result.Classified.Surface) + len(result.Classified.Flow)
	if totalClassified != 3 {
		t.Errorf("Expected 3 total classified, got %d", totalClassified)
	}

	if !result.AgentEnabled {
		t.Error("Expected AgentEnabled to be true")
	}
}
