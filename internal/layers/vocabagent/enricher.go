package vocabagent

import (
	"context"
	"fmt"
	"strings"
	"unicode"
)

// VocabEnricher is a layer that enriches vocabulary by:
// 1. Tokenizing incoming events
// 2. Checking tokens against SQL vocab storage
// 3. Collecting unknown tokens
// 4. Calling VocabAgent to classify unknown words
// 5. Storing AI-classified results back to SQL
// 6. Passing through original input to the next layer
type VocabEnricher struct {
	storage VocabStorage
	agent   *VocabAgent
	debug   bool
}

// NewVocabEnricher creates a new VocabEnricher layer
func NewVocabEnricher(storage VocabStorage, agent *VocabAgent, debug bool) *VocabEnricher {
	return &VocabEnricher{
		storage: storage,
		agent:   agent,
		debug:   debug,
	}
}

// Name returns the layer identifier
func (v *VocabEnricher) Name() string {
	return "vocab_enricher"
}

// Process implements the Layer interface
// It extracts words from events, finds unknown ones, classifies them via AI,
// stores them, and passes through the original input
func (v *VocabEnricher) Process(input interface{}) (interface{}, error) {
	// If storage or agent is not configured, pass through
	if v.storage == nil || v.agent == nil {
		if v.debug {
			fmt.Println("[DEBUG] [VocabEnricher] Storage or agent not configured, passing through")
		}
		return input, nil
	}

	// If agent is disabled, pass through
	if !v.agent.config.Enabled || v.agent.config.APIKey == "" {
		if v.debug {
			fmt.Println("[DEBUG] [VocabEnricher] VocabAgent disabled, passing through")
		}
		return input, nil
	}

	ctx := context.Background()

	// Extract all tokens from input
	tokens := v.extractTokens(input)
	if len(tokens) == 0 {
		if v.debug {
			fmt.Println("[DEBUG] [VocabEnricher] No tokens extracted, passing through")
		}
		return input, nil
	}

	if v.debug {
		fmt.Printf("[DEBUG] [VocabEnricher] Extracted %d unique tokens\n", len(tokens))
	}

	// Find tokens that are NOT in the vocab storage
	unknownTokens, err := v.findUnknownTokens(ctx, tokens)
	if err != nil {
		if v.debug {
			fmt.Printf("[DEBUG] [VocabEnricher] Error finding unknown tokens: %v\n", err)
		}
		// Continue anyway, don't block the pipeline
		return input, nil
	}

	if len(unknownTokens) == 0 {
		if v.debug {
			fmt.Println("[DEBUG] [VocabEnricher] All tokens known, passing through")
		}
		return input, nil
	}

	if v.debug {
		fmt.Printf("[DEBUG] [VocabEnricher] Found %d unknown tokens: %v\n", len(unknownTokens), unknownTokens)
	}

	// Call VocabAgent to classify unknown tokens
	result, err := v.agent.ClassifyWords(unknownTokens)
	if err != nil {
		if v.debug {
			fmt.Printf("[DEBUG] [VocabEnricher] VocabAgent classification failed: %v\n", err)
		}
		return input, nil
	}

	if result.Error != "" {
		if v.debug {
			fmt.Printf("[DEBUG] [VocabEnricher] VocabAgent returned error: %s\n", result.Error)
		}
		return input, nil
	}

	// Store the classified words back to SQL
	if result.Classified != nil {
		storedCount, err := v.storeClassifiedWords(ctx, result.Classified)
		if err != nil {
			if v.debug {
				fmt.Printf("[DEBUG] [VocabEnricher] Failed to store classified words: %v\n", err)
			}
		} else if storedCount > 0 {
			fmt.Printf("[VocabEnricher] Stored %d new vocabulary entries from AI classification\n", storedCount)
		}
	}

	// Pass through the original input unchanged
	return input, nil
}

// extractTokens extracts all unique tokens from the input
func (v *VocabEnricher) extractTokens(input interface{}) []string {
	tokens := make(map[string]bool)

	switch val := input.(type) {
	case string:
		for _, t := range v.tokenize(val) {
			tokens[t] = true
		}

	case []string:
		for _, s := range val {
			for _, t := range v.tokenize(s) {
				tokens[t] = true
			}
		}

	case map[string]interface{}:
		v.extractTokensFromMap(val, tokens)

	case []map[string]interface{}:
		for _, m := range val {
			v.extractTokensFromMap(m, tokens)
		}
	}

	// Convert map to slice
	result := make([]string, 0, len(tokens))
	for t := range tokens {
		// Filter out very short tokens and pure numbers
		if len(t) > 1 && !isNumeric(t) {
			result = append(result, t)
		}
	}

	return result
}

// extractTokensFromMap extracts tokens from event map fields
func (v *VocabEnricher) extractTokensFromMap(m map[string]interface{}, tokens map[string]bool) {
	// Fields that typically contain event names/actions
	eventFields := []string{"event", "event_name", "eventName", "name", "action", "type"}

	for _, field := range eventFields {
		if val, ok := m[field]; ok {
			if strVal, ok := val.(string); ok {
				for _, t := range v.tokenize(strVal) {
					tokens[t] = true
				}
			}
		}
	}
}

// tokenize splits a string into individual tokens (mirrors eventadapter logic)
func (v *VocabEnricher) tokenize(s string) []string {
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
	normalized = v.splitCamelCase(normalized)

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
func (v *VocabEnricher) splitCamelCase(s string) string {
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

// findUnknownTokens returns tokens that are NOT in the vocab storage
func (v *VocabEnricher) findUnknownTokens(ctx context.Context, tokens []string) ([]string, error) {
	unknown := make([]string, 0)

	for _, token := range tokens {
		entry, err := v.storage.GetVocab(ctx, token)
		if err != nil {
			return nil, fmt.Errorf("error checking vocab for '%s': %w", token, err)
		}

		// If entry is nil, the token is not in vocab
		if entry == nil {
			unknown = append(unknown, token)
		}
	}

	return unknown, nil
}

// storeClassifiedWords stores the AI-classified words to the vocab storage
func (v *VocabEnricher) storeClassifiedWords(ctx context.Context, classified *VocabData) (int, error) {
	entries := make([]*VocabEntry, 0)

	// Add status words
	for _, word := range classified.Status {
		entries = append(entries, &VocabEntry{
			Word:       word,
			Category:   CategoryStatus,
			Normalized: word, // Use the word itself as normalized form
			Source:     "ai",
		})
	}

	// Add surface words
	for _, word := range classified.Surface {
		entries = append(entries, &VocabEntry{
			Word:       word,
			Category:   CategorySurface,
			Normalized: word,
			Source:     "ai",
		})
	}

	// Add flow words
	for _, word := range classified.Flow {
		entries = append(entries, &VocabEntry{
			Word:       word,
			Category:   CategoryFlow,
			Normalized: word,
			Source:     "ai",
		})
	}

	if len(entries) == 0 {
		return 0, nil
	}

	// Batch upsert
	if err := v.storage.UpsertVocabBatch(ctx, entries); err != nil {
		return 0, err
	}

	return len(entries), nil
}

// isNumeric checks if a string is purely numeric
func isNumeric(s string) bool {
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}
