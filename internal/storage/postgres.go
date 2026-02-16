package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/velum/internal/config"
	_ "github.com/lib/pq"
)

// PostgresStorage implements Storage interface with PostgreSQL persistence
type PostgresStorage struct {
	db            *sql.DB
	retentionDays int
	connStr       string
}

// NewPostgresStorage creates a new PostgreSQL-backed storage
func NewPostgresStorage(config *config.PostgresStorageConfig, retentionDays int) (*PostgresStorage, error) {
	if config == nil {
		return nil, fmt.Errorf("postgres config is nil")
	}

	// Build connection string
	connStr := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		config.Host,
		config.Port,
		config.User,
		config.Password,
		config.Database,
		config.SSLMode,
	)

	// Open database connection
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(config.MaxConnections)
	db.SetMaxIdleConns(config.MaxConnections / 2)
	db.SetConnMaxLifetime(time.Hour)

	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	storage := &PostgresStorage{
		db:            db,
		retentionDays: retentionDays,
		connStr:       connStr,
	}

	// Initialize schema
	if err := storage.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return storage, nil
}

// initSchema creates the required tables if they don't exist
func (s *PostgresStorage) initSchema() error {
	schema := `
		CREATE TABLE IF NOT EXISTS pattern_snapshots (
			date DATE NOT NULL,
			pattern_type TEXT NOT NULL,
			flow TEXT NOT NULL,

			affected_users INTEGER NOT NULL,
			total_flows INTEGER NOT NULL,
			impact_ratio REAL NOT NULL,

			severity TEXT NOT NULL,
			confidence TEXT,
			pattern_version TEXT NOT NULL,

			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,

			PRIMARY KEY (date, pattern_type, flow)
		);

		CREATE INDEX IF NOT EXISTS idx_pattern_snapshots_lookup 
		ON pattern_snapshots (pattern_type, flow, date);
	`

	_, err := s.db.Exec(schema)
	return err
}

// StoreSnapshot persists a pattern snapshot (upsert)
func (s *PostgresStorage) StoreSnapshot(ctx context.Context, snapshot *PatternSnapshot) error {
	query := `
		INSERT INTO pattern_snapshots (
			date,
			pattern_type,
			flow,
			affected_users,
			total_flows,
			impact_ratio,
			severity,
			confidence,
			pattern_version
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT(date, pattern_type, flow)
		DO UPDATE SET
			affected_users = EXCLUDED.affected_users,
			total_flows = EXCLUDED.total_flows,
			impact_ratio = EXCLUDED.impact_ratio,
			severity = EXCLUDED.severity,
			confidence = EXCLUDED.confidence,
			pattern_version = EXCLUDED.pattern_version
	`

	dateStr := snapshot.Date.Format("2006-01-02")

	_, err := s.db.ExecContext(ctx, query,
		dateStr,
		snapshot.PatternType,
		snapshot.Flow,
		snapshot.AffectedUsers,
		snapshot.TotalFlows,
		snapshot.ImpactRatio,
		snapshot.Severity,
		snapshot.Confidence,
		snapshot.PatternVersion,
	)

	return err
}

// FetchBaselineSnapshots retrieves historical snapshots for baseline computation
func (s *PostgresStorage) FetchBaselineSnapshots(
	ctx context.Context,
	patternType, flow string,
	endDate time.Time,
	windowDays int,
) ([]*PatternSnapshot, error) {
	startDate := endDate.AddDate(0, 0, -windowDays)
	// End date is exclusive (up to yesterday)
	endDateExclusive := endDate.AddDate(0, 0, -1)

	query := `
		SELECT 
			date,
			pattern_type,
			flow,
			affected_users,
			total_flows,
			impact_ratio,
			severity,
			confidence,
			pattern_version
		FROM pattern_snapshots
		WHERE pattern_type = $1
		  AND flow = $2
		  AND date >= $3
		  AND date <= $4
		ORDER BY date ASC
	`

	startDateStr := startDate.Format("2006-01-02")
	endDateStr := endDateExclusive.Format("2006-01-02")

	rows, err := s.db.QueryContext(ctx, query,
		patternType,
		flow,
		startDateStr,
		endDateStr,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snapshots []*PatternSnapshot
	for rows.Next() {
		var dateStr string
		var confidence sql.NullString
		snap := &PatternSnapshot{}

		err := rows.Scan(
			&dateStr,
			&snap.PatternType,
			&snap.Flow,
			&snap.AffectedUsers,
			&snap.TotalFlows,
			&snap.ImpactRatio,
			&snap.Severity,
			&confidence,
			&snap.PatternVersion,
		)
		if err != nil {
			return nil, err
		}

		// Parse date
		snap.Date, _ = time.Parse("2006-01-02", dateStr)
		if confidence.Valid {
			snap.Confidence = confidence.String
		}
		snapshots = append(snapshots, snap)
	}

	return snapshots, rows.Err()
}

// Cleanup removes snapshots older than retention period
func (s *PostgresStorage) Cleanup(ctx context.Context) (int64, error) {
	if s.retentionDays <= 0 {
		return 0, nil // No cleanup if retention is disabled
	}

	cutoffDate := time.Now().UTC().AddDate(0, 0, -s.retentionDays)
	cutoffStr := cutoffDate.Format("2006-01-02")

	query := `DELETE FROM pattern_snapshots WHERE date < $1`

	result, err := s.db.ExecContext(ctx, query, cutoffStr)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}

// Close closes the database connection
func (s *PostgresStorage) Close() error {
	return s.db.Close()
}

// GetStats returns storage statistics (implements Stats interface)
func (s *PostgresStorage) GetStats(ctx context.Context) (map[string]interface{}, error) {
	stats := make(map[string]interface{})
	stats["storage_type"] = "postgresql"
	stats["retention_days"] = s.retentionDays

	// Total snapshots
	var totalCount int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pattern_snapshots").Scan(&totalCount)
	if err != nil {
		return nil, err
	}
	stats["total_snapshots"] = totalCount

	// Distinct patterns
	var patternCount int
	err = s.db.QueryRowContext(ctx, "SELECT COUNT(DISTINCT pattern_type) FROM pattern_snapshots").Scan(&patternCount)
	if err != nil {
		return nil, err
	}
	stats["distinct_patterns"] = patternCount

	// Distinct flows
	var flowCount int
	err = s.db.QueryRowContext(ctx, "SELECT COUNT(DISTINCT flow) FROM pattern_snapshots").Scan(&flowCount)
	if err != nil {
		return nil, err
	}
	stats["distinct_flows"] = flowCount

	// Date range
	var minDate, maxDate sql.NullString
	err = s.db.QueryRowContext(ctx, "SELECT MIN(date)::text, MAX(date)::text FROM pattern_snapshots").Scan(&minDate, &maxDate)
	if err != nil {
		return nil, err
	}
	if minDate.Valid {
		stats["oldest_date"] = minDate.String
	}
	if maxDate.Valid {
		stats["newest_date"] = maxDate.String
	}

	return stats, nil
}
