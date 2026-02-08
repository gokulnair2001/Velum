package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteStorage(t *testing.T) {
	// Create temp directory for test DB
	tmpDir, err := os.MkdirTemp("", "velum-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	config := &SQLiteConfig{
		DBPath:        dbPath,
		RetentionDays: 90,
	}

	store, err := NewSQLiteStorage(config)
	if err != nil {
		t.Fatalf("Failed to create SQLite storage: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Test storing a snapshot
	snapshot := &PatternSnapshot{
		Date:           time.Now().UTC().Truncate(24 * time.Hour),
		PatternType:    "retry_storm",
		Flow:           "checkout",
		AffectedUsers:  50,
		TotalFlows:     100,
		ImpactRatio:    0.35,
		Severity:       "high",
		Confidence:     "medium",
		PatternVersion: "v1",
	}

	err = store.StoreSnapshot(ctx, snapshot)
	if err != nil {
		t.Fatalf("Failed to store snapshot: %v", err)
	}

	// Verify stats
	stats, err := store.GetStats(ctx)
	if err != nil {
		t.Fatalf("Failed to get stats: %v", err)
	}

	if stats["total_snapshots"] != 1 {
		t.Errorf("Expected 1 snapshot, got %v", stats["total_snapshots"])
	}
}

func TestSQLiteFetchBaseline(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "velum-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := &SQLiteConfig{
		DBPath:        filepath.Join(tmpDir, "test.db"),
		RetentionDays: 90,
	}

	store, err := NewSQLiteStorage(config)
	if err != nil {
		t.Fatalf("Failed to create SQLite storage: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	baseDate := time.Now().UTC().Truncate(24 * time.Hour)

	// Store 10 days of snapshots
	for i := 1; i <= 10; i++ {
		snapshot := &PatternSnapshot{
			Date:           baseDate.AddDate(0, 0, -i),
			PatternType:    "retry_storm",
			Flow:           "checkout",
			AffectedUsers:  50,
			TotalFlows:     100,
			ImpactRatio:    0.30 + float64(i)*0.01, // Varying ratios
			Severity:       "medium",
			PatternVersion: "v1",
		}
		if err := store.StoreSnapshot(ctx, snapshot); err != nil {
			t.Fatalf("Failed to store snapshot: %v", err)
		}
	}

	// Fetch baseline for last 7 days
	snapshots, err := store.FetchBaselineSnapshots(ctx, "retry_storm", "checkout", baseDate, 7)
	if err != nil {
		t.Fatalf("Failed to fetch baseline: %v", err)
	}

	// Should get at least 6 days (day -1 to day -7, excluding today)
	if len(snapshots) < 6 {
		t.Errorf("Expected at least 6 snapshots, got %d", len(snapshots))
	}
}

func TestSQLiteUpsert(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "velum-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := &SQLiteConfig{
		DBPath:        filepath.Join(tmpDir, "test.db"),
		RetentionDays: 90,
	}

	store, err := NewSQLiteStorage(config)
	if err != nil {
		t.Fatalf("Failed to create SQLite storage: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	date := time.Now().UTC().Truncate(24 * time.Hour)

	// Store initial snapshot
	snapshot1 := &PatternSnapshot{
		Date:           date,
		PatternType:    "retry_storm",
		Flow:           "checkout",
		AffectedUsers:  50,
		TotalFlows:     100,
		ImpactRatio:    0.30,
		Severity:       "medium",
		PatternVersion: "v1",
	}
	if err := store.StoreSnapshot(ctx, snapshot1); err != nil {
		t.Fatalf("Failed to store snapshot: %v", err)
	}

	// Upsert with updated values
	snapshot2 := &PatternSnapshot{
		Date:           date,
		PatternType:    "retry_storm",
		Flow:           "checkout",
		AffectedUsers:  75,
		TotalFlows:     150,
		ImpactRatio:    0.45,
		Severity:       "high",
		PatternVersion: "v1",
	}
	if err := store.StoreSnapshot(ctx, snapshot2); err != nil {
		t.Fatalf("Failed to upsert snapshot: %v", err)
	}

	// Verify only 1 row exists
	stats, err := store.GetStats(ctx)
	if err != nil {
		t.Fatalf("Failed to get stats: %v", err)
	}

	if stats["total_snapshots"] != 1 {
		t.Errorf("Expected 1 snapshot after upsert, got %v", stats["total_snapshots"])
	}
}

func TestSQLiteCleanup(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "velum-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := &SQLiteConfig{
		DBPath:        filepath.Join(tmpDir, "test.db"),
		RetentionDays: 7, // Only keep 7 days
	}

	store, err := NewSQLiteStorage(config)
	if err != nil {
		t.Fatalf("Failed to create SQLite storage: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	baseDate := time.Now().UTC().Truncate(24 * time.Hour)

	// Store 20 days of snapshots
	for i := 1; i <= 20; i++ {
		snapshot := &PatternSnapshot{
			Date:           baseDate.AddDate(0, 0, -i),
			PatternType:    "retry_storm",
			Flow:           "checkout",
			ImpactRatio:    0.30,
			PatternVersion: "v1",
		}
		if err := store.StoreSnapshot(ctx, snapshot); err != nil {
			t.Fatalf("Failed to store snapshot: %v", err)
		}
	}

	// Run cleanup
	deleted, err := store.Cleanup(ctx)
	if err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}

	// Should have deleted snapshots older than 7 days
	if deleted < 10 {
		t.Errorf("Expected at least 10 deleted, got %d", deleted)
	}
}

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
