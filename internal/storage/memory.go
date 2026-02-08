package storage

import (
	"context"
	"time"
)

// InMemoryStorage is a simple in-memory storage for testing/development
type InMemoryStorage struct {
	snapshots []*PatternSnapshot
}

// NewInMemoryStorage creates a new in-memory storage
func NewInMemoryStorage() *InMemoryStorage {
	return &InMemoryStorage{
		snapshots: make([]*PatternSnapshot, 0),
	}
}

// FetchBaselineSnapshots retrieves snapshots from memory
func (s *InMemoryStorage) FetchBaselineSnapshots(ctx context.Context, patternType, flow string, endDate time.Time, windowDays int) ([]*PatternSnapshot, error) {
	startDate := endDate.AddDate(0, 0, -windowDays)
	// End date is exclusive (up to yesterday)
	endDateExclusive := endDate.AddDate(0, 0, -1)

	var results []*PatternSnapshot
	for _, snap := range s.snapshots {
		if snap.PatternType == patternType &&
			snap.Flow == flow &&
			!snap.Date.Before(startDate) &&
			!snap.Date.After(endDateExclusive) {
			results = append(results, snap)
		}
	}

	return results, nil
}

// StoreSnapshot adds a snapshot to memory
func (s *InMemoryStorage) StoreSnapshot(ctx context.Context, snapshot *PatternSnapshot) error {
	// Check for existing snapshot and update it
	for i, existing := range s.snapshots {
		if existing.PatternType == snapshot.PatternType &&
			existing.Flow == snapshot.Flow &&
			existing.Date.Format("2006-01-02") == snapshot.Date.Format("2006-01-02") {
			s.snapshots[i] = snapshot
			return nil
		}
	}

	s.snapshots = append(s.snapshots, snapshot)
	return nil
}

// Cleanup removes old snapshots (no-op for in-memory, just returns 0)
func (s *InMemoryStorage) Cleanup(ctx context.Context) (int64, error) {
	return 0, nil
}

// Close is a no-op for in-memory storage
func (s *InMemoryStorage) Close() error {
	return nil
}

// GetAllSnapshots returns all stored snapshots (for testing)
func (s *InMemoryStorage) GetAllSnapshots() []*PatternSnapshot {
	return s.snapshots
}

// GetStats returns storage statistics
func (s *InMemoryStorage) GetStats(ctx context.Context) (map[string]interface{}, error) {
	stats := make(map[string]interface{})
	stats["storage_type"] = "in_memory"
	stats["total_snapshots"] = len(s.snapshots)

	// Count distinct patterns and flows
	patterns := make(map[string]bool)
	flows := make(map[string]bool)
	for _, snap := range s.snapshots {
		patterns[snap.PatternType] = true
		flows[snap.Flow] = true
	}
	stats["distinct_patterns"] = len(patterns)
	stats["distinct_flows"] = len(flows)

	return stats, nil
}
