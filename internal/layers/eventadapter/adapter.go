package eventadapter

import (
	"encoding/json"
	"strings"
	"unicode"
)

// NormalizedEvent represents a cleaned and categorized event
type NormalizedEvent struct {
	Original      string   `json:"original"`
	Tokens        []string `json:"tokens"`
	Status        []string `json:"status,omitempty"`
	Surface       []string `json:"surface,omitempty"`
	Flow          []string `json:"flow,omitempty"`
	Uncategorized []string `json:"uncategorized,omitempty"`
}

// ProcessedEvent represents the full output with original fields preserved
type ProcessedEvent struct {
	// Original fields from input (id, user_id, ts, etc.)
	OriginalFields map[string]interface{} `json:"-"`

	// The raw event string
	Event string `json:"event"`

	// Normalized breakdown of the event
	Normalized *NormalizedEvent `json:"normalized"`
}

// MarshalJSON custom marshaler to flatten original fields into output
func (p *ProcessedEvent) MarshalJSON() ([]byte, error) {
	// Start with original fields
	result := make(map[string]interface{})
	for k, v := range p.OriginalFields {
		result[k] = v
	}

	// Add normalized data
	result["normalized"] = p.Normalized

	return json.Marshal(result)
}

// EventAdapter is the first layer - cleans raw events into consumable data
type EventAdapter struct {
	vocab *Vocabulary
}

// New creates a new EventAdapter with default vocabulary
func New() *EventAdapter {
	return &EventAdapter{
		vocab: NewVocabulary(),
	}
}

// NewWithVocabulary creates an EventAdapter with custom vocabulary
func NewWithVocabulary(vocab *Vocabulary) *EventAdapter {
	return &EventAdapter{
		vocab: vocab,
	}
}

// Name returns the layer identifier
func (e *EventAdapter) Name() string {
	return "event_adapter"
}

// Process implements the Layer interface
func (e *EventAdapter) Process(input interface{}) (interface{}, error) {
	switch v := input.(type) {
	case string:
		return e.NormalizeEventString(v), nil
	case []string:
		results := make([]*NormalizedEvent, len(v))
		for i, event := range v {
			results[i] = e.NormalizeEventString(event)
		}
		return results, nil
	case map[string]interface{}:
		if !e.isValidEvent(v) {
			return nil, nil // Skip invalid events
		}
		return e.processEventMap(v), nil
	case []map[string]interface{}:
		results := make([]*ProcessedEvent, 0, len(v))
		for _, event := range v {
			if e.isValidEvent(event) {
				results = append(results, e.processEventMap(event))
			}
		}
		return results, nil
	default:
		return input, nil
	}
}

// isValidEvent checks if the event has the mandatory fields: id and ts
func (e *EventAdapter) isValidEvent(event map[string]interface{}) bool {
	// Check for id field
	if _, hasID := event["id"]; !hasID {
		return false
	}

	// Check for ts field
	if _, hasTS := event["ts"]; !hasTS {
		return false
	}

	return true
}

// processEventMap handles a map-based event and preserves all original fields
func (e *EventAdapter) processEventMap(event map[string]interface{}) *ProcessedEvent {
	processed := &ProcessedEvent{
		OriginalFields: make(map[string]interface{}),
	}

	// Copy all original fields
	for k, v := range event {
		processed.OriginalFields[k] = v
	}

	// Look for event name field and normalize it
	eventNameFields := []string{"event", "event_name", "eventName", "name", "action", "type"}

	for _, field := range eventNameFields {
		if val, ok := event[field]; ok {
			if strVal, ok := val.(string); ok {
				processed.Event = strVal
				processed.Normalized = e.NormalizeEventString(strVal)
				break
			}
		}
	}

	return processed
}

// NormalizeEventString tokenizes and categorizes a single event string
func (e *EventAdapter) NormalizeEventString(eventStr string) *NormalizedEvent {
	tokens := e.tokenize(eventStr)

	normalized := &NormalizedEvent{
		Original:      eventStr,
		Tokens:        tokens,
		Status:        make([]string, 0),
		Surface:       make([]string, 0),
		Flow:          make([]string, 0),
		Uncategorized: make([]string, 0),
	}

	seen := make(map[string]bool)

	for _, token := range tokens {
		lower := strings.ToLower(token)

		// Skip noise words
		if e.vocab.Noise[lower] {
			continue
		}

		// Skip if already processed
		if seen[lower] {
			continue
		}
		seen[lower] = true

		// Categorize the token
		categorized := false

		if norm, ok := e.vocab.Status[lower]; ok {
			if !contains(normalized.Status, norm) {
				normalized.Status = append(normalized.Status, norm)
			}
			categorized = true
		}

		if norm, ok := e.vocab.Surface[lower]; ok {
			if !contains(normalized.Surface, norm) {
				normalized.Surface = append(normalized.Surface, norm)
			}
			categorized = true
		}

		if norm, ok := e.vocab.Flow[lower]; ok {
			if !contains(normalized.Flow, norm) {
				normalized.Flow = append(normalized.Flow, norm)
			}
			categorized = true
		}

		if !categorized && len(lower) > 1 {
			normalized.Uncategorized = append(normalized.Uncategorized, lower)
		}
	}

	return normalized
}

// tokenize splits an event string into individual tokens
func (e *EventAdapter) tokenize(s string) []string {
	// Replace common delimiters with spaces
	normalized := strings.Map(func(r rune) rune {
		switch r {
		case '_', '-', '.', '/', ':', '|', ',', ';':
			return ' '
		default:
			return r
		}
	}, s)

	// Handle camelCase and PascalCase
	normalized = splitCamelCase(normalized)

	// Split by whitespace and filter empty strings
	parts := strings.Fields(normalized)
	tokens := make([]string, 0, len(parts))

	for _, part := range parts {
		cleaned := strings.TrimSpace(part)
		if len(cleaned) > 0 {
			tokens = append(tokens, strings.ToLower(cleaned))
		}
	}

	return tokens
}

// splitCamelCase inserts spaces before uppercase letters in camelCase strings
func splitCamelCase(s string) string {
	var result strings.Builder
	
	for i, r := range s {
		if i > 0 && unicode.IsUpper(r) {
			prev := rune(s[i-1])
			if unicode.IsLower(prev) || unicode.IsDigit(prev) {
				result.WriteRune(' ')
			}
		}
		result.WriteRune(r)
	}

	return result.String()
}

// contains checks if a slice contains a string
func contains(slice []string, str string) bool {
	for _, s := range slice {
		if s == str {
			return true
		}
	}
	return false
}

// GetVocabulary returns the current vocabulary for inspection/modification
func (e *EventAdapter) GetVocabulary() *Vocabulary {
	return e.vocab
}
