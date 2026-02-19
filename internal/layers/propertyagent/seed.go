package propertyagent

import (
	"context"
	"fmt"

	"github.com/velum/internal/canonical"
)

// SeedBuiltinDimensions seeds the property registry with known dimension fields
// from canonical.BuiltinDimensions. This should be called once on server startup.
// It is idempotent — skips seeding if entries already exist.
func SeedBuiltinDimensions(ctx context.Context, storage PropertyStorage) error {
	// Check if already seeded
	stats, ok := storage.(PropertyStats)
	if ok {
		propStats, err := stats.GetPropertyStats(ctx)
		if err == nil {
			if totalEntries, exists := propStats["total_entries"]; exists {
				if count, ok := totalEntries.(int); ok && count > 0 {
					fmt.Printf("Property registry already seeded (%d entries), skipping\n", count)
					return nil
				}
			}
		}
	}

	// Build entries from canonical.BuiltinDimensions
	entries := make([]*PropertyEntry, 0, len(canonical.BuiltinDimensions))
	seen := make(map[string]bool)

	for fieldName, normalizedLabel := range canonical.BuiltinDimensions {
		if seen[fieldName] {
			continue
		}
		seen[fieldName] = true

		entries = append(entries, &PropertyEntry{
			KeyName: fieldName,
			Role:    RoleDimension,
			Label:   normalizedLabel,
			Source:  "builtin",
		})
	}

	// Batch upsert
	if err := storage.UpsertPropertyBatch(ctx, entries); err != nil {
		return fmt.Errorf("failed to seed property registry: %w", err)
	}

	fmt.Printf("Seeded property registry with %d built-in dimension entries\n", len(entries))
	return nil
}

// GetBuiltinDimensionCount returns the count of built-in dimension mappings.
func GetBuiltinDimensionCount() int {
	return len(canonical.BuiltinDimensions)
}
