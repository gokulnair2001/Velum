package vocabagent

import (
	"context"
	"testing"
)

func TestInMemoryVocabStorage(t *testing.T) {
	storage := NewInMemoryVocabStorage()
	defer storage.Close()

	ctx := context.Background()

	t.Run("UpsertAndGetVocab", func(t *testing.T) {
		entry := &VocabEntry{
			Word:       "checkout",
			Category:   CategoryFlow,
			Normalized: "checkout",
			Source:     "ai",
		}

		err := storage.UpsertVocab(ctx, entry)
		if err != nil {
			t.Fatalf("Failed to upsert vocab: %v", err)
		}

		retrieved, err := storage.GetVocab(ctx, "checkout")
		if err != nil {
			t.Fatalf("Failed to get vocab: %v", err)
		}

		if retrieved == nil {
			t.Fatal("Expected entry, got nil")
		}

		if retrieved.Word != "checkout" {
			t.Errorf("Expected word 'checkout', got '%s'", retrieved.Word)
		}

		if retrieved.Category != CategoryFlow {
			t.Errorf("Expected category 'flow', got '%s'", retrieved.Category)
		}

		if retrieved.Normalized != "checkout" {
			t.Errorf("Expected normalized 'checkout', got '%s'", retrieved.Normalized)
		}

		if retrieved.Source != "ai" {
			t.Errorf("Expected source 'ai', got '%s'", retrieved.Source)
		}
	})

	t.Run("UpsertUpdatesExisting", func(t *testing.T) {
		// First insert
		entry := &VocabEntry{
			Word:       "testword",
			Category:   CategoryStatus,
			Normalized: "test",
			Source:     "ai",
		}
		storage.UpsertVocab(ctx, entry)

		// Update with new values
		updated := &VocabEntry{
			Word:       "testword",
			Category:   CategorySurface,
			Normalized: "updated_test",
			Source:     "ai",
		}
		err := storage.UpsertVocab(ctx, updated)
		if err != nil {
			t.Fatalf("Failed to update vocab: %v", err)
		}

		retrieved, _ := storage.GetVocab(ctx, "testword")
		if retrieved.Category != CategorySurface {
			t.Errorf("Expected category 'surface', got '%s'", retrieved.Category)
		}
		if retrieved.Normalized != "updated_test" {
			t.Errorf("Expected normalized 'updated_test', got '%s'", retrieved.Normalized)
		}
	})

	t.Run("GetVocabNotFound", func(t *testing.T) {
		entry, err := storage.GetVocab(ctx, "nonexistent")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if entry != nil {
			t.Error("Expected nil for nonexistent word")
		}
	})

	t.Run("UpsertVocabBatch", func(t *testing.T) {
		entries := []*VocabEntry{
			{Word: "batch1", Category: CategoryStatus, Normalized: "view", Source: "ai"},
			{Word: "batch2", Category: CategorySurface, Normalized: "modal", Source: "ai"},
			{Word: "batch3", Category: CategoryFlow, Normalized: "authentication", Source: "ai"},
		}

		err := storage.UpsertVocabBatch(ctx, entries)
		if err != nil {
			t.Fatalf("Failed to upsert batch: %v", err)
		}

		for _, e := range entries {
			retrieved, _ := storage.GetVocab(ctx, e.Word)
			if retrieved == nil {
				t.Errorf("Expected to find word '%s'", e.Word)
			}
		}
	})

	t.Run("GetVocabByCategory", func(t *testing.T) {
		// Add some test data
		storage.UpsertVocab(ctx, &VocabEntry{Word: "click", Category: CategoryStatus, Normalized: "click", Source: "ai"})
		storage.UpsertVocab(ctx, &VocabEntry{Word: "tap", Category: CategoryStatus, Normalized: "click", Source: "ai"})

		entries, err := storage.GetVocabByCategory(ctx, CategoryStatus)
		if err != nil {
			t.Fatalf("Failed to get by category: %v", err)
		}

		if len(entries) < 2 {
			t.Errorf("Expected at least 2 status entries, got %d", len(entries))
		}

		for _, e := range entries {
			if e.Category != CategoryStatus {
				t.Errorf("Expected category 'status', got '%s'", e.Category)
			}
		}
	})

	t.Run("GetAllVocab", func(t *testing.T) {
		entries, err := storage.GetAllVocab(ctx)
		if err != nil {
			t.Fatalf("Failed to get all vocab: %v", err)
		}

		if len(entries) == 0 {
			t.Error("Expected non-empty results")
		}
	})

	t.Run("GetVocabData", func(t *testing.T) {
		// Add diverse test data
		storage.UpsertVocab(ctx, &VocabEntry{Word: "success", Category: CategoryStatus, Normalized: "success", Source: "ai"})
		storage.UpsertVocab(ctx, &VocabEntry{Word: "button", Category: CategorySurface, Normalized: "button", Source: "ai"})
		storage.UpsertVocab(ctx, &VocabEntry{Word: "login", Category: CategoryFlow, Normalized: "authentication", Source: "ai"})

		data, err := storage.GetVocabData(ctx)
		if err != nil {
			t.Fatalf("Failed to get vocab data: %v", err)
		}

		if len(data.Status) == 0 {
			t.Error("Expected non-empty status list")
		}
		if len(data.Surface) == 0 {
			t.Error("Expected non-empty surface list")
		}
		if len(data.Flow) == 0 {
			t.Error("Expected non-empty flow list")
		}
	})

	t.Run("DeleteVocab", func(t *testing.T) {
		// Add then delete
		storage.UpsertVocab(ctx, &VocabEntry{Word: "todelete", Category: CategoryStatus, Normalized: "delete", Source: "ai"})

		err := storage.DeleteVocab(ctx, "todelete")
		if err != nil {
			t.Fatalf("Failed to delete: %v", err)
		}

		entry, _ := storage.GetVocab(ctx, "todelete")
		if entry != nil {
			t.Error("Expected word to be deleted")
		}
	})

	t.Run("GetVocabStats", func(t *testing.T) {
		stats, err := storage.GetVocabStats(ctx)
		if err != nil {
			t.Fatalf("Failed to get stats: %v", err)
		}

		if stats["storage_type"] != "in_memory" {
			t.Errorf("Expected storage_type 'in_memory', got '%v'", stats["storage_type"])
		}

		if _, ok := stats["total_entries"]; !ok {
			t.Error("Expected total_entries in stats")
		}

		if _, ok := stats["by_category"]; !ok {
			t.Error("Expected by_category in stats")
		}

		if _, ok := stats["by_source"]; !ok {
			t.Error("Expected by_source in stats")
		}
	})
}

func TestVocabCategory(t *testing.T) {
	if CategoryStatus != "status" {
		t.Errorf("Expected 'status', got '%s'", CategoryStatus)
	}
	if CategorySurface != "surface" {
		t.Errorf("Expected 'surface', got '%s'", CategorySurface)
	}
	if CategoryFlow != "flow" {
		t.Errorf("Expected 'flow', got '%s'", CategoryFlow)
	}
}

func TestVocabData(t *testing.T) {
	data := &VocabData{
		Status:  []string{"view", "click", "success"},
		Surface: []string{"button", "modal", "card"},
		Flow:    []string{"authentication", "checkout"},
	}

	if len(data.Status) != 3 {
		t.Errorf("Expected 3 status, got %d", len(data.Status))
	}
	if len(data.Surface) != 3 {
		t.Errorf("Expected 3 surface, got %d", len(data.Surface))
	}
	if len(data.Flow) != 2 {
		t.Errorf("Expected 2 flow, got %d", len(data.Flow))
	}
}

func TestSeedBuiltinVocabulary(t *testing.T) {
	storage := NewInMemoryVocabStorage()
	defer storage.Close()

	ctx := context.Background()

	// First seed should populate the storage
	err := SeedBuiltinVocabulary(ctx, storage)
	if err != nil {
		t.Fatalf("First seed failed: %v", err)
	}

	// Verify entries were created
	stats, err := storage.GetVocabStats(ctx)
	if err != nil {
		t.Fatalf("Failed to get stats: %v", err)
	}

	totalEntries := stats["total_entries"].(int)
	if totalEntries == 0 {
		t.Error("Expected entries after seeding, got 0")
	}

	// Note: Some words exist in multiple categories (e.g., "view" in status and surface).
	// Since word is the primary key, the actual count may be less than the sum of all mappings.
	// This is expected behavior - upsert semantics.
	if totalEntries < 100 {
		t.Errorf("Expected at least 100 unique entries, got %d", totalEntries)
	}

	initialCount := totalEntries

	// Second seed should be idempotent (skipped because data already exists)
	err = SeedBuiltinVocabulary(ctx, storage)
	if err != nil {
		t.Fatalf("Second seed failed: %v", err)
	}

	// Verify count is still the same (no duplicates)
	stats2, _ := storage.GetVocabStats(ctx)
	totalAfterSecondSeed := stats2["total_entries"].(int)

	if totalAfterSecondSeed != initialCount {
		t.Errorf("Expected same count after second seed (%d), got %d", initialCount, totalAfterSecondSeed)
	}

	// Verify we can retrieve some known entries
	entry, err := storage.GetVocab(ctx, "click")
	if err != nil {
		t.Fatalf("Failed to get vocab: %v", err)
	}
	if entry == nil {
		t.Error("Expected 'click' entry to exist")
	} else {
		if entry.Category != CategoryStatus {
			t.Errorf("Expected 'click' to be status, got %s", entry.Category)
		}
		if entry.Source != "builtin" {
			t.Errorf("Expected source 'builtin', got %s", entry.Source)
		}
	}

	// Verify all three categories have entries
	byCategory := stats["by_category"].(map[string]int)
	if byCategory["status"] < 10 {
		t.Errorf("Expected at least 10 status entries, got %d", byCategory["status"])
	}
	if byCategory["surface"] < 10 {
		t.Errorf("Expected at least 10 surface entries, got %d", byCategory["surface"])
	}
	if byCategory["flow"] < 10 {
		t.Errorf("Expected at least 10 flow entries, got %d", byCategory["flow"])
	}
}

func TestGetBuiltinVocabCount(t *testing.T) {
	status, surface, flow := GetBuiltinVocabCount()

	// Just verify we get reasonable counts (the actual vocab has many entries)
	if status < 10 {
		t.Errorf("Expected at least 10 status entries, got %d", status)
	}
	if surface < 10 {
		t.Errorf("Expected at least 10 surface entries, got %d", surface)
	}
	if flow < 10 {
		t.Errorf("Expected at least 10 flow entries, got %d", flow)
	}
}
