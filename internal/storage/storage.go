package storage

import (
	"context"
	"time"
)

// PatternSnapshot represents a historical snapshot of a pattern
// This is the canonical data model for storage
type PatternSnapshot struct {
	Date           time.Time `json:"date"`
	PatternType    string    `json:"pattern_type"`
	Flow           string    `json:"flow"`
	AffectedUsers  int       `json:"affected_users"`
	TotalFlows     int       `json:"total_flows"`
	ImpactRatio    float64   `json:"impact_ratio"`
	Severity       string    `json:"severity"`
	Confidence     string    `json:"confidence"`
	PatternVersion string    `json:"pattern_version"`
}

// Storage defines the interface for baseline data persistence
// Any storage backend (PostgreSQL, etc.) must implement this
type Storage interface {
	// FetchBaselineSnapshots retrieves historical snapshots for a pattern+flow
	// Used for baseline computation
	FetchBaselineSnapshots(ctx context.Context, patternType, flow string, endDate time.Time, windowDays int) ([]*PatternSnapshot, error)

	// StoreSnapshot persists a pattern snapshot (upsert semantics)
	// Idempotent - safe for retries
	StoreSnapshot(ctx context.Context, snapshot *PatternSnapshot) error

	// Cleanup removes snapshots older than retention period
	// Returns number of deleted rows
	Cleanup(ctx context.Context) (int64, error)

	// Close closes the storage connection
	Close() error
}

// Stats provides optional statistics about storage
type Stats interface {
	GetStats(ctx context.Context) (map[string]interface{}, error)
}
