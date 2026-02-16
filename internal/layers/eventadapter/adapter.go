package eventadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

// VocabLookup defines an interface for external vocabulary lookup.
// This allows EventAdapter to use vocabulary stored in SQLite
// without creating circular dependencies.
type VocabLookup interface {
	// LookupWord returns the category for a word, or empty string if not found.
	// Categories are: "status", "surface", "flow"
	LookupWord(ctx context.Context, word string) (category string, err error)
}

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
	vocab       *Vocabulary
	vocabLookup VocabLookup // Optional external vocab lookup (e.g., PostgreSQL)
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

// NewWithVocabLookup creates an EventAdapter with external vocabulary lookup
func NewWithVocabLookup(lookup VocabLookup) *EventAdapter {
	return &EventAdapter{
		vocab:       NewVocabulary(),
		vocabLookup: lookup,
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
		if err := e.validateEvent(v, 0); err != nil {
			return nil, err
		}
		return e.processEventMap(v), nil
	case []map[string]interface{}:
		results := make([]*ProcessedEvent, 0, len(v))
		for i, event := range v {
			if err := e.validateEvent(event, i); err != nil {
				return nil, err
			}
			results = append(results, e.processEventMap(event))
		}
		return results, nil
	default:
		return input, nil
	}
}

// validateEvent checks if the event has mandatory fields (id, ts) and returns an error if missing
func (e *EventAdapter) validateEvent(event map[string]interface{}, index int) error {
	_, hasID := event["id"]
	_, hasTS := event["ts"]

	if !hasID && !hasTS {
		return fmt.Errorf("event at index %d missing mandatory fields: id, ts", index)
	}
	if !hasID {
		return fmt.Errorf("event at index %d missing mandatory field: id", index)
	}
	if !hasTS {
		return fmt.Errorf("event at index %d missing mandatory field: ts", index)
	}

	return nil
}

// isValidEvent returns true if the event has all mandatory fields (id, ts)
func (e *EventAdapter) isValidEvent(event map[string]interface{}) bool {
	return e.validateEvent(event, 0) == nil
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

		// Skip noise words (kept in memory as a simple filter)
		if e.vocab.Noise[lower] {
			continue
		}

		// Skip if already processed
		if seen[lower] {
			continue
		}
		seen[lower] = true

		// Use SQLite as the single source of truth for all vocabulary
		// (built-in vocabulary is seeded to SQLite at startup)
		categorized := false

		if e.vocabLookup != nil {
			if category, err := e.vocabLookup.LookupWord(context.Background(), lower); err == nil && category != "" {
				switch category {
				case "status":
					if !contains(normalized.Status, lower) {
						normalized.Status = append(normalized.Status, lower)
					}
					categorized = true
				case "surface":
					if !contains(normalized.Surface, lower) {
						normalized.Surface = append(normalized.Surface, lower)
					}
					categorized = true
				case "flow":
					if !contains(normalized.Flow, lower) {
						normalized.Flow = append(normalized.Flow, lower)
					}
					categorized = true
				}
			}
		} else {
			// Fallback to static vocabulary if SQLite not configured (backward compatibility)
			if norm, ok := e.vocab.Status[lower]; ok {
				if !contains(normalized.Status, norm) {
					normalized.Status = append(normalized.Status, norm)
				}
				categorized = true
			} else if norm, ok := e.vocab.Surface[lower]; ok {
				if !contains(normalized.Surface, norm) {
					normalized.Surface = append(normalized.Surface, norm)
				}
				categorized = true
			} else if norm, ok := e.vocab.Flow[lower]; ok {
				if !contains(normalized.Flow, norm) {
					normalized.Flow = append(normalized.Flow, norm)
				}
				categorized = true
			}
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
