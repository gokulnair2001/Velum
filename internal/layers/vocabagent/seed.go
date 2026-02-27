package vocabagent

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/velum/internal/layers/eventadapter"
)

// SeedBuiltinVocabulary seeds the storage with built-in vocabulary from eventadapter.
// This should be called once on server startup.
// It performs an incremental upsert — new builtin words are added and existing
// builtin words are updated, while AI-learned words are left untouched.
func SeedBuiltinVocabulary(ctx context.Context, storage VocabStorage) error {
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

	// Batch upsert all entries (ON CONFLICT updates builtin entries)
	if err := storage.UpsertVocabBatch(ctx, entries); err != nil {
		return fmt.Errorf("failed to seed vocabulary: %w", err)
	}

	slog.Info("seeded vocabulary with built-in entries",
		"total", len(entries),
		"status", len(vocab.Status),
		"surface", len(vocab.Surface),
		"flow", len(vocab.Flow),
	)

	return nil
}

// GetBuiltinVocabCount returns the count of built-in vocabulary entries
func GetBuiltinVocabCount() (status, surface, flow int) {
	vocab := eventadapter.NewVocabulary()
	return len(vocab.Status), len(vocab.Surface), len(vocab.Flow)
}
