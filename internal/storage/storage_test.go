package storage

import (
	"context"
	"testing"
	"time"
)

func TestInMemoryStorage(t *testing.T) {
	store := NewInMemoryStorage()
	ctx := context.Background()

	// Store snapshot
	snapshot := &PatternSnapshot{
		Date:           time.Now().UTC().Truncate(24 * time.Hour),
		PatternType:    "retry_storm",
		Flow:           "checkout",
		ImpactRatio:    0.35,
		PatternVersion: "v1",
	}

	err := store.StoreSnapshot(ctx, snapshot)
	if err != nil {
		t.Fatalf("Failed to store snapshot: %v", err)
	}

	// Verify
	snapshots := store.GetAllSnapshots()
	if len(snapshots) != 1 {
		t.Errorf("Expected 1 snapshot, got %d", len(snapshots))
	}

	// Test stats
	stats, err := store.GetStats(ctx)
	if err != nil {
		t.Fatalf("Failed to get stats: %v", err)
	}

	if stats["storage_type"] != "in_memory" {
		t.Errorf("Expected storage_type 'in_memory', got %v", stats["storage_type"])
	}
}
