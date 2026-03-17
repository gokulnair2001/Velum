package storage

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	_ "github.com/lib/pq"
	"github.com/velum/internal/config"
)

// PostgresStorage implements Storage interface with PostgreSQL persistence.
// Each project gets its own table (pattern_snapshots_{project_id}) for full isolation.
type PostgresStorage struct {
	db            *sql.DB
	retentionDays int
	connStr       string

	// ensuredTables tracks which project tables have been created this session
	ensuredTables map[string]bool
	ensureMu      sync.Mutex
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
	db.SetConnMaxIdleTime(5 * time.Minute)

	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	storage := &PostgresStorage{
		db:            db,
		retentionDays: retentionDays,
		connStr:       connStr,
		ensuredTables: make(map[string]bool),
	}

	return storage, nil
}

// tableName returns the project-specific table name.
// Project IDs are validated at the HTTP layer ([a-zA-Z0-9_-]{1,64}),
// so we just normalize to a safe Postgres identifier.
func tableName(projectID string) string {
	safe := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			return r
		}
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A') // toLower
		}
		if r == '-' {
			return '_'
		}
		return -1 // drop unexpected characters
	}, projectID)
	if safe == "" {
		safe = "default"
	}
	return "pattern_snapshots_" + safe
}

// ensureProjectTable creates the table for a project if it doesn't exist yet.
// Uses an in-memory set to avoid repeated DDL calls within the same process lifetime.
func (s *PostgresStorage) ensureProjectTable(projectID string) error {
	tbl := tableName(projectID)

	s.ensureMu.Lock()
	defer s.ensureMu.Unlock()

	if s.ensuredTables[tbl] {
		return nil
	}

	schema := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			date DATE NOT NULL,
			pattern_type TEXT NOT NULL,
			flow TEXT NOT NULL,
			context_key TEXT NOT NULL DEFAULT '',

			affected_users INTEGER NOT NULL,
			total_flows INTEGER NOT NULL,
			impact_ratio REAL NOT NULL,

			severity TEXT NOT NULL,
			confidence TEXT,
			pattern_version TEXT NOT NULL,

			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,

			PRIMARY KEY (date, pattern_type, flow, context_key)
		);

		CREATE INDEX IF NOT EXISTS idx_%s_lookup
		ON %s (pattern_type, flow, context_key, date);
	`, tbl, tbl, tbl)

	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("failed to create table %s: %w", tbl, err)
	}

	s.ensuredTables[tbl] = true
	return nil
}

// StoreSnapshot persists a pattern snapshot (upsert)
func (s *PostgresStorage) StoreSnapshot(ctx context.Context, snapshot *PatternSnapshot) error {
	projectID := snapshot.ProjectID
	if projectID == "" {
		projectID = "default"
	}

	if err := s.ensureProjectTable(projectID); err != nil {
		return err
	}

	tbl := tableName(projectID)
	query := fmt.Sprintf(`
		INSERT INTO %s (
			date,
			pattern_type,
			flow,
			context_key,
			affected_users,
			total_flows,
			impact_ratio,
			severity,
			confidence,
			pattern_version
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT(date, pattern_type, flow, context_key)
		DO UPDATE SET
			affected_users = EXCLUDED.affected_users,
			total_flows = EXCLUDED.total_flows,
			impact_ratio = EXCLUDED.impact_ratio,
			severity = EXCLUDED.severity,
			confidence = EXCLUDED.confidence,
			pattern_version = EXCLUDED.pattern_version
	`, tbl)

	dateStr := snapshot.Date.Format("2006-01-02")

	_, err := s.db.ExecContext(ctx, query,
		dateStr,
		snapshot.PatternType,
		snapshot.Flow,
		snapshot.ContextKey,
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
	projectID, patternType, flow, contextKey string,
	endDate time.Time,
	windowDays int,
) ([]*PatternSnapshot, error) {
	if projectID == "" {
		projectID = "default"
	}

	if err := s.ensureProjectTable(projectID); err != nil {
		return nil, err
	}

	startDate := endDate.AddDate(0, 0, -windowDays)
	endDateExclusive := endDate.AddDate(0, 0, -1)
	tbl := tableName(projectID)

	query := fmt.Sprintf(`
		SELECT 
			date,
			pattern_type,
			flow,
			context_key,
			affected_users,
			total_flows,
			impact_ratio,
			severity,
			confidence,
			pattern_version
		FROM %s
		WHERE pattern_type = $1
		  AND flow = $2
		  AND context_key = $3
		  AND date >= $4
		  AND date <= $5
		ORDER BY date ASC
	`, tbl)

	startDateStr := startDate.Format("2006-01-02")
	endDateStr := endDateExclusive.Format("2006-01-02")

	rows, err := s.db.QueryContext(ctx, query,
		patternType,
		flow,
		contextKey,
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
		snap := &PatternSnapshot{ProjectID: projectID}

		err := rows.Scan(
			&dateStr,
			&snap.PatternType,
			&snap.Flow,
			&snap.ContextKey,
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

		snap.Date, _ = time.Parse("2006-01-02", dateStr)
		if confidence.Valid {
			snap.Confidence = confidence.String
		}
		snapshots = append(snapshots, snap)
	}

	return snapshots, rows.Err()
}

// Cleanup removes snapshots older than retention period across all project tables
func (s *PostgresStorage) Cleanup(ctx context.Context) (int64, error) {
	if s.retentionDays <= 0 {
		return 0, nil
	}

	// Find all project tables
	rows, err := s.db.QueryContext(ctx,
		`SELECT tablename FROM pg_tables WHERE schemaname = 'public' AND tablename LIKE 'pattern_snapshots_%'`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return 0, err
		}
		tables = append(tables, t)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	cutoffStr := time.Now().UTC().AddDate(0, 0, -s.retentionDays).Format("2006-01-02")
	var totalDeleted int64

	for _, tbl := range tables {
		query := fmt.Sprintf(`DELETE FROM %s WHERE date < $1`, tbl)
		result, err := s.db.ExecContext(ctx, query, cutoffStr)
		if err != nil {
			slog.Warn("cleanup failed for table", "table", tbl, "error", err)
			continue
		}
		if n, err := result.RowsAffected(); err == nil {
			totalDeleted += n
		}
	}

	return totalDeleted, nil
}

// Ping checks database connectivity
func (s *PostgresStorage) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
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

	// Count project tables
	rows, err := s.db.QueryContext(ctx,
		`SELECT tablename FROM pg_tables WHERE schemaname = 'public' AND tablename LIKE 'pattern_snapshots_%'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		tables = append(tables, t)
	}
	stats["project_tables"] = len(tables)

	// Aggregate counts across all project tables
	var totalSnapshots int
	for _, tbl := range tables {
		var count int
		q := fmt.Sprintf(`SELECT COUNT(*) FROM %s`, tbl)
		if err := s.db.QueryRowContext(ctx, q).Scan(&count); err == nil {
			totalSnapshots += count
		}
	}
	stats["total_snapshots"] = totalSnapshots

	return stats, nil
}
