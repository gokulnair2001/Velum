package eventadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/velum/internal/canonical"
)

// VocabLookup defines an interface for external vocabulary lookup.
// This allows EventAdapter to use vocabulary stored in PostgreSQL
// without creating circular dependencies.
type VocabLookup interface {
	// LookupWord returns the category and normalized value for a word.
	// Returns empty strings if not found.
	// Categories are: "status", "surface", "flow"
	LookupWord(ctx context.Context, word string) (category string, normalized string, err error)
}

// PropertyLookup defines an interface for external property registry lookup.
// This allows EventAdapter to build canonical context from the property registry
// without creating circular dependencies with the propertyagent package.
type PropertyLookup interface {
	// LookupProperty returns the role and label for a property key.
	// Returns empty role if not found.
	// Roles are: "dimension", "target", "condition", "measure"
	LookupProperty(ctx context.Context, keyName string) (role string, label string, err error)
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

	// Context holds the canonical property classification.
	// Built by EventAdapter from property registry DB lookups — same pattern as vocabulary.
	Context *canonical.EventContext `json:"context,omitempty"`
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

	// Add canonical context if present
	if p.Context != nil && !p.Context.IsEmpty() {
		result["context"] = p.Context
	}

	return json.Marshal(result)
}

// EventAdapter is the first layer - cleans raw events into consumable data
type EventAdapter struct {
	vocab          *Vocabulary
	vocabLookup    VocabLookup    // Optional external vocab lookup (e.g., PostgreSQL)
	propertyLookup PropertyLookup // Optional external property registry lookup
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

// NewWithLookups creates an EventAdapter with both vocabulary and property registry lookups.
// This is the recommended constructor when both vocab and property storage are available.
func NewWithLookups(vocabLookup VocabLookup, propertyLookup PropertyLookup) *EventAdapter {
	return &EventAdapter{
		vocab:          NewVocabulary(),
		vocabLookup:    vocabLookup,
		propertyLookup: propertyLookup,
	}
}

// Name returns the layer identifier
func (e *EventAdapter) Name() string {
	return "event_adapter"
}

// Process implements the Layer interface
func (e *EventAdapter) Process(input interface{}) (interface{}, error) {
	return e.processWithCtx(context.Background(), input)
}

// ProcessWithContext implements the ContextAwareLayer interface.
// Extracts the request context so DB lookups respect cancellation.
func (e *EventAdapter) ProcessWithContext(input interface{}, metadata interface{}) (interface{}, error) {
	ctx := context.Background()
	type contextProvider interface{ RequestContext() context.Context }
	if cp, ok := metadata.(contextProvider); ok {
		ctx = cp.RequestContext()
	}
	return e.processWithCtx(ctx, input)
}

func (e *EventAdapter) processWithCtx(ctx context.Context, input interface{}) (interface{}, error) {
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
		return e.processEventMapCtx(ctx, v), nil
	case []map[string]interface{}:
		results := make([]*ProcessedEvent, 0, len(v))
		for i, event := range v {
			if err := e.validateEvent(event, i); err != nil {
				return nil, err
			}
			results = append(results, e.processEventMapCtx(ctx, event))
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
	return e.processEventMapCtx(context.Background(), event)
}

// processEventMapCtx is like processEventMap but propagates context to DB lookups.
func (e *EventAdapter) processEventMapCtx(ctx context.Context, event map[string]interface{}) *ProcessedEvent {
	processed := &ProcessedEvent{
		OriginalFields: make(map[string]interface{}),
	}

	// Copy all original fields
	for k, v := range event {
		processed.OriginalFields[k] = v
	}

	// Build canonical context from property registry (same pattern as vocab lookup)
	processed.Context = e.buildEventContextCtx(ctx, event)

	// Look for event name field and normalize it
	eventNameFields := []string{"event", "event_name", "eventName", "name", "action", "type"}

	for _, field := range eventNameFields {
		if val, ok := event[field]; ok {
			if strVal, ok := val.(string); ok {
				processed.Event = strVal
				processed.Normalized = e.normalizeEventStringCtx(ctx, strVal)
				break
			}
		}
	}

	return processed
}

// NormalizeEventString tokenizes and categorizes a single event string
func (e *EventAdapter) NormalizeEventString(eventStr string) *NormalizedEvent {
	return e.normalizeEventStringCtx(context.Background(), eventStr)
}

// normalizeEventStringCtx is like NormalizeEventString but propagates context to DB lookups.
func (e *EventAdapter) normalizeEventStringCtx(ctx context.Context, eventStr string) *NormalizedEvent {
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

		// Use PostgreSQL as the primary source of truth for all vocabulary
		// (built-in vocabulary is seeded to PostgreSQL at startup, AI-learned
		// words are added by VocabEnricher). Fall back to the in-memory static
		// vocabulary if PostgreSQL doesn't have the word — this provides a
		// safety net during DB issues and guarantees correct synonym mappings
		// (e.g., player → playback) that AI-learned vocab may lack.
		categorized := false

		if e.vocabLookup != nil {
			if category, norm, err := e.vocabLookup.LookupWord(ctx, lower); err == nil && category != "" {
				// Use the normalized value if available, fall back to original word
				val := norm
				if val == "" {
					val = lower
				}
				switch category {
				case "status":
					if !contains(normalized.Status, val) {
						normalized.Status = append(normalized.Status, val)
					}
					categorized = true
				case "surface":
					if !contains(normalized.Surface, val) {
						normalized.Surface = append(normalized.Surface, val)
					}
					categorized = true
				case "flow":
					if !contains(normalized.Flow, val) {
						normalized.Flow = append(normalized.Flow, val)
					}
					categorized = true
				}
			}
		}

		// Fallback to static vocabulary if DB lookup didn't categorize the word
		// (DB not configured, word not found, or DB error)
		if !categorized {
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

// buildEventContext iterates over all event properties and builds an EventContext
// by looking up each property from the property registry DB.
// Same pattern as vocabulary: each property is looked up individually from DB.
//   - Core fields (event, user_id, etc.) → skipped
//   - Built-in dimensions (device, country, etc.) → dimension (no DB needed)
//   - Numeric values → measure (type inference, no DB needed)
//   - DB registry hit → apply cached role (target or condition)
//   - Unknown (no DB entry) → skipped (ContextEnricher will learn it for next time)
func (e *EventAdapter) buildEventContext(event map[string]interface{}) *canonical.EventContext {
	return e.buildEventContextCtx(context.Background(), event)
}

func (e *EventAdapter) buildEventContextCtx(ctx context.Context, event map[string]interface{}) *canonical.EventContext {
	ec := canonical.NewEventContext()

	for key, value := range event {
		// Skip core fields
		if canonical.IsCoreField(key) {
			continue
		}

		// 1. Check built-in dimensions (deterministic, no DB)
		if label, isDim := canonical.IsDimension(key); isDim {
			ec.Dimensions[label] = canonical.FormatSampleValue(value)
			continue
		}

		// 2. Check if numeric → measure (type inference)
		if numVal, isMeasure := canonical.IsMeasureValue(value); isMeasure {
			ec.Measures[key] = numVal
			continue
		}

		// 3. Look up from property registry DB (like vocab lookup)
		if e.propertyLookup != nil {
			role, _, err := e.propertyLookup.LookupProperty(ctx, key)
			if err == nil && role != "" {
				switch canonical.PropertyRole(role) {
				case canonical.RoleDimension:
					ec.Dimensions[key] = canonical.FormatSampleValue(value)
				case canonical.RoleTarget:
					ec.Targets[key] = value
				case canonical.RoleCondition:
					ec.Conditions[key] = value
				case canonical.RoleMeasure:
					if numVal, ok := canonical.IsMeasureValue(value); ok {
						ec.Measures[key] = numVal
					}
				}
			}
		}
	}

	if ec.IsEmpty() {
		return nil
	}
	return ec
}
