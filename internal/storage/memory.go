package storage

import (
	"context"
	"sync"
	"time"
)

// InMemoryStorage is a simple in-memory storage for testing/development.
// Data is isolated per project using a map.
type InMemoryStorage struct {
	mu        sync.RWMutex
	snapshots map[string][]*PatternSnapshot // keyed by projectID
}

// NewInMemoryStorage creates a new in-memory storage
func NewInMemoryStorage() *InMemoryStorage {
	return &InMemoryStorage{
		snapshots: make(map[string][]*PatternSnapshot),
	}
}

// FetchBaselineSnapshots retrieves snapshots from memory
func (s *InMemoryStorage) FetchBaselineSnapshots(ctx context.Context, projectID, patternType, flow, contextKey string, endDate time.Time, windowDays int) ([]*PatternSnapshot, error) {
	startDate := endDate.AddDate(0, 0, -windowDays)
	endDateExclusive := endDate.AddDate(0, 0, -1)

	if projectID == "" {
		projectID = "default"
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []*PatternSnapshot
	for _, snap := range s.snapshots[projectID] {
		if snap.PatternType == patternType &&
			snap.Flow == flow &&
			snap.ContextKey == contextKey &&
			!snap.Date.Before(startDate) &&
			!snap.Date.After(endDateExclusive) {
			results = append(results, snap)
		}
	}

	return results, nil
}

// StoreSnapshot adds a snapshot to memory
func (s *InMemoryStorage) StoreSnapshot(ctx context.Context, snapshot *PatternSnapshot) error {
	pid := snapshot.ProjectID
	if pid == "" {
		pid = "default"
		snapshot.ProjectID = pid
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check for existing snapshot and update it
	for i, existing := range s.snapshots[pid] {
		if existing.PatternType == snapshot.PatternType &&
			existing.Flow == snapshot.Flow &&
			existing.ContextKey == snapshot.ContextKey &&
			existing.Date.Format("2006-01-02") == snapshot.Date.Format("2006-01-02") {
			s.snapshots[pid][i] = snapshot
			return nil
		}
	}

	s.snapshots[pid] = append(s.snapshots[pid], snapshot)
	return nil
}

// Cleanup removes old snapshots (no-op for in-memory, just returns 0)
func (s *InMemoryStorage) Cleanup(ctx context.Context) (int64, error) {
	return 0, nil
}

// Ping is a no-op for in-memory storage
func (s *InMemoryStorage) Ping(ctx context.Context) error {
	return nil
}

// Close is a no-op for in-memory storage
func (s *InMemoryStorage) Close() error {
	return nil
}

// GetAllSnapshots returns all stored snapshots across all projects (for testing)
func (s *InMemoryStorage) GetAllSnapshots() []*PatternSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var all []*PatternSnapshot
	for _, snaps := range s.snapshots {
		all = append(all, snaps...)
	}
	return all
}

// GetStats returns storage statistics
func (s *InMemoryStorage) GetStats(ctx context.Context) (map[string]interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := make(map[string]interface{})
	stats["storage_type"] = "in_memory"

	total := 0
	patterns := make(map[string]bool)
	flows := make(map[string]bool)
	for _, snaps := range s.snapshots {
		total += len(snaps)
		for _, snap := range snaps {
			patterns[snap.PatternType] = true
			flows[snap.Flow] = true
		}
	}
	stats["total_snapshots"] = total
	stats["project_count"] = len(s.snapshots)
	stats["distinct_patterns"] = len(patterns)
	stats["distinct_flows"] = len(flows)

	return stats, nil
}
