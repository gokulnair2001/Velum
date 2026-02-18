package propertyagent

import (
	"context"
	"fmt"

	"github.com/velum/internal/canonical"
)

// ContextEnricher is a pipeline layer that discovers unknown event properties
// and classifies them via AI, storing results to DB.
//
// This layer is learn-only (like VocabEnricher). It does NOT build or attach
// context to events. The actual context building happens in EventAdapter,
// which looks up each property from the DB — same pattern as vocabulary.
//
// Flow:
//  1. For each event, iterate over extra properties (skip core fields).
//  2. Skip BuiltinDimensions (already seeded to DB at startup).
//  3. Skip numeric values (measures are type-inferred at runtime by EventAdapter).
//  4. Check DB property_registry → already classified, skip.
//  5. Collect remaining unknowns → batch to PropertyAgent AI.
//  6. Store AI results back to DB.
//  7. Pass through original input unchanged to the next layer.
type ContextEnricher struct {
	storage PropertyStorage
	agent   *PropertyAgent
	debug   bool
}

// NewContextEnricher creates a new ContextEnricher layer.
func NewContextEnricher(storage PropertyStorage, agent *PropertyAgent, debug bool) *ContextEnricher {
	return &ContextEnricher{
		storage: storage,
		agent:   agent,
		debug:   debug,
	}
}

// Name returns the layer identifier.
func (e *ContextEnricher) Name() string {
	return "context_enricher"
}

// Process implements the Layer interface.
// It discovers unknown properties, classifies via AI, stores to DB,
// and passes through the original input unchanged.
func (e *ContextEnricher) Process(input interface{}) (interface{}, error) {
	// If storage or agent is not configured, pass through
	if e.storage == nil || e.agent == nil {
		if e.debug {
			fmt.Println("[DEBUG] [ContextEnricher] Storage or agent not configured, passing through")
		}
		return input, nil
	}

	// If agent is disabled, pass through
	if !e.agent.config.Enabled || e.agent.config.APIKey == "" {
		if e.debug {
			fmt.Println("[DEBUG] [ContextEnricher] PropertyAgent disabled, passing through")
		}
		return input, nil
	}

	// Work with []map[string]interface{} (batch of events)
	events, ok := input.([]map[string]interface{})
	if !ok {
		if e.debug {
			fmt.Println("[DEBUG] [ContextEnricher] Input is not []map[string]interface{}, passing through")
		}
		return input, nil
	}

	if len(events) == 0 {
		return input, nil
	}

	ctx := context.Background()

	// Collect all unique extra property keys across all events with sample values.
	// Only collect string-valued properties that aren't core fields, dimensions, or numeric.
	unknownKeys := make(map[string]string) // key → sample value string

	for _, event := range events {
		for key, value := range event {
			// Skip core fields
			if canonical.IsCoreField(key) {
				continue
			}

			// Skip built-in dimensions (already seeded to DB)
			if _, isDim := canonical.IsDimension(key); isDim {
				continue
			}

			// Skip numeric values (measures are type-inferred at runtime)
			if _, isMeasure := canonical.IsMeasureValue(value); isMeasure {
				continue
			}

			// Check DB registry — if already classified, skip
			entry, err := e.storage.GetProperty(ctx, key)
			if err != nil {
				if e.debug {
					fmt.Printf("[DEBUG] [ContextEnricher] Error looking up property '%s': %v\n", key, err)
				}
				continue
			}

			if entry != nil {
				// Already classified in DB, nothing to learn
				continue
			}

			// Unknown — collect for AI batch classification
			if _, exists := unknownKeys[key]; !exists {
				unknownKeys[key] = canonical.FormatSampleValue(value)
			}
		}
	}

	// Batch classify unknowns via AI (if any)
	if len(unknownKeys) > 0 {
		if e.debug {
			fmt.Printf("[DEBUG] [ContextEnricher] %d unknown property keys to classify\n", len(unknownKeys))
		}

		if err := e.classifyAndStore(ctx, unknownKeys); err != nil {
			if e.debug {
				fmt.Printf("[DEBUG] [ContextEnricher] AI classification failed: %v\n", err)
			}
			// Don't block the pipeline
		}
	} else if e.debug {
		fmt.Println("[DEBUG] [ContextEnricher] All property keys already known, passing through")
	}

	// Pass through original input unchanged (like VocabEnricher)
	return input, nil
}

// classifyAndStore sends unknowns to AI and stores results in DB.
func (e *ContextEnricher) classifyAndStore(ctx context.Context, unknownKeys map[string]string) error {
	// Build the batch for AI
	properties := make([]UnknownProperty, 0, len(unknownKeys))
	for key, sample := range unknownKeys {
		properties = append(properties, UnknownProperty{Key: key, SampleValue: sample})
	}

	// Call AI
	result, err := e.agent.ClassifyProperties(properties)
	if err != nil {
		return err
	}

	// Build entries from AI result
	entries := make([]*PropertyEntry, 0)

	for _, key := range result.Target {
		entries = append(entries, &PropertyEntry{
			KeyName:     key,
			Role:        RoleTarget,
			Label:       key,
			SampleValue: unknownKeys[key],
			Source:      "ai",
		})
	}

	for _, key := range result.Condition {
		entries = append(entries, &PropertyEntry{
			KeyName:     key,
			Role:        RoleCondition,
			Label:       key,
			SampleValue: unknownKeys[key],
			Source:      "ai",
		})
	}

	// Store to DB
	if len(entries) > 0 {
		if err := e.storage.UpsertPropertyBatch(ctx, entries); err != nil {
			if e.debug {
				fmt.Printf("[DEBUG] [ContextEnricher] Failed to store classified properties: %v\n", err)
			}
			return err
		}
		fmt.Printf("Context: Learned and stored %d new property classifications from AI\n", len(entries))
	}

	return nil
}
