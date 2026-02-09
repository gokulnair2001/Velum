package vocabagent

import (
	"context"
	"fmt"

	"github.com/velum/internal/layers/eventadapter"
)

// SeedBuiltinVocabulary seeds the storage with built-in vocabulary from eventadapter.
// This should be called once on server startup.
// It checks if vocabulary already exists and only seeds if empty (idempotent).
func SeedBuiltinVocabulary(ctx context.Context, storage VocabStorage) error {
	// Check if vocabulary is already seeded
	stats, ok := storage.(VocabStats)
	if ok {
		vocabStats, err := stats.GetVocabStats(ctx)
		if err == nil {
			if totalEntries, exists := vocabStats["total_entries"]; exists {
				if count, ok := totalEntries.(int); ok && count > 0 {
					fmt.Printf("Vocabulary already seeded (%d entries), skipping\n", count)
					return nil
				}
			}
		}
	}

	// Get the built-in vocabulary
	vocab := eventadapter.NewVocabulary()

	// Convert to VocabEntry slice
	entries := make([]*VocabEntry, 0)

	// Add Status mappings
	for keyword, normalized := range vocab.Status {
		entries = append(entries, &VocabEntry{
			Word:       keyword,
			Category:   CategoryStatus,
			Normalized: normalized,
			Source:     "builtin",
		})
	}

	// Add Surface mappings
	for keyword, normalized := range vocab.Surface {
		entries = append(entries, &VocabEntry{
			Word:       keyword,
			Category:   CategorySurface,
			Normalized: normalized,
			Source:     "builtin",
		})
	}

	// Add Flow mappings
	for keyword, normalized := range vocab.Flow {
		entries = append(entries, &VocabEntry{
			Word:       keyword,
			Category:   CategoryFlow,
			Normalized: normalized,
			Source:     "builtin",
		})
	}

	// Batch upsert all entries
	if err := storage.UpsertVocabBatch(ctx, entries); err != nil {
		return fmt.Errorf("failed to seed vocabulary: %w", err)
	}

	fmt.Printf("Seeded vocabulary with %d built-in entries (status: %d, surface: %d, flow: %d)\n",
		len(entries), len(vocab.Status), len(vocab.Surface), len(vocab.Flow))

	return nil
}

// GetBuiltinVocabCount returns the count of built-in vocabulary entries
func GetBuiltinVocabCount() (status, surface, flow int) {
	vocab := eventadapter.NewVocabulary()
	return len(vocab.Status), len(vocab.Surface), len(vocab.Flow)
}
