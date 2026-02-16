package vocabagent

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"
	"github.com/velum/internal/config"
)

// PostgresVocabStorage implements VocabStorage interface with PostgreSQL persistence
// It stores vocabulary in the same PostgreSQL database used for baseline storage,
// in a separate "vocabulary" table.
type PostgresVocabStorage struct {
	db      *sql.DB
	connStr string
}

// NewPostgresVocabStorage creates a new PostgreSQL-backed vocabulary storage.
// It connects to the same database configured in the main storage config.
func NewPostgresVocabStorage(cfg *config.PostgresStorageConfig) (*PostgresVocabStorage, error) {
	if cfg == nil {
		return nil, fmt.Errorf("postgres config is nil")
	}

	// Build connection string
	connStr := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Host,
		cfg.Port,
		cfg.User,
		cfg.Password,
		cfg.Database,
		cfg.SSLMode,
	)

	// Open database connection
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	maxConns := cfg.MaxConnections
	if maxConns == 0 {
		maxConns = 10
	}
	db.SetMaxOpenConns(maxConns)
	db.SetMaxIdleConns(maxConns / 2)
	db.SetConnMaxLifetime(time.Hour)

	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	storage := &PostgresVocabStorage{
		db:      db,
		connStr: connStr,
	}

	// Initialize schema
	if err := storage.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize vocabulary schema: %w", err)
	}

	return storage, nil
}

// initSchema creates the vocabulary table if it doesn't exist
func (s *PostgresVocabStorage) initSchema() error {
	schema := `
		CREATE TABLE IF NOT EXISTS vocabulary (
			word TEXT PRIMARY KEY NOT NULL,
			category TEXT NOT NULL CHECK(category IN ('status', 'surface', 'flow')),
			normalized TEXT NOT NULL,
			source TEXT NOT NULL DEFAULT 'ai',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE INDEX IF NOT EXISTS idx_vocabulary_category 
		ON vocabulary (category);

		CREATE INDEX IF NOT EXISTS idx_vocabulary_normalized 
		ON vocabulary (category, normalized);
	`

	_, err := s.db.Exec(schema)
	return err
}

// UpsertVocab stores or updates a vocabulary entry
func (s *PostgresVocabStorage) UpsertVocab(ctx context.Context, entry *VocabEntry) error {
	query := `
		INSERT INTO vocabulary (word, category, normalized, source, created_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT(word)
		DO UPDATE SET
			category = EXCLUDED.category,
			normalized = EXCLUDED.normalized,
			source = EXCLUDED.source
	`

	createdAt := entry.CreatedAt
	if createdAt == "" {
		createdAt = time.Now().UTC().Format(time.RFC3339)
	}

	_, err := s.db.ExecContext(ctx, query,
		entry.Word,
		entry.Category,
		entry.Normalized,
		entry.Source,
		createdAt,
	)

	return err
}

// UpsertVocabBatch stores multiple vocabulary entries in a single transaction
func (s *PostgresVocabStorage) UpsertVocabBatch(ctx context.Context, entries []*VocabEntry) error {
	if len(entries) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	query := `
		INSERT INTO vocabulary (word, category, normalized, source, created_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT(word)
		DO UPDATE SET
			category = EXCLUDED.category,
			normalized = EXCLUDED.normalized,
			source = EXCLUDED.source
	`

	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	now := time.Now().UTC().Format(time.RFC3339)

	for _, entry := range entries {
		createdAt := entry.CreatedAt
		if createdAt == "" {
			createdAt = now
		}

		_, err := stmt.ExecContext(ctx, entry.Word, entry.Category, entry.Normalized, entry.Source, createdAt)
		if err != nil {
			return fmt.Errorf("failed to upsert word '%s': %w", entry.Word, err)
		}
	}

	return tx.Commit()
}

// GetVocab retrieves a specific vocabulary entry by word
func (s *PostgresVocabStorage) GetVocab(ctx context.Context, word string) (*VocabEntry, error) {
	query := `
		SELECT word, category, normalized, source, created_at
		FROM vocabulary
		WHERE word = $1
	`

	var entry VocabEntry
	err := s.db.QueryRowContext(ctx, query, word).Scan(
		&entry.Word,
		&entry.Category,
		&entry.Normalized,
		&entry.Source,
		&entry.CreatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &entry, nil
}

// GetVocabByCategory retrieves all vocabulary entries for a specific category
func (s *PostgresVocabStorage) GetVocabByCategory(ctx context.Context, category VocabCategory) ([]*VocabEntry, error) {
	query := `
		SELECT word, category, normalized, source, created_at
		FROM vocabulary
		WHERE category = $1
		ORDER BY word ASC
	`

	rows, err := s.db.QueryContext(ctx, query, category)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*VocabEntry
	for rows.Next() {
		var entry VocabEntry
		err := rows.Scan(&entry.Word, &entry.Category, &entry.Normalized, &entry.Source, &entry.CreatedAt)
		if err != nil {
			return nil, err
		}
		entries = append(entries, &entry)
	}

	return entries, rows.Err()
}

// GetAllVocab retrieves all vocabulary entries
func (s *PostgresVocabStorage) GetAllVocab(ctx context.Context) ([]*VocabEntry, error) {
	query := `
		SELECT word, category, normalized, source, created_at
		FROM vocabulary
		ORDER BY category, word ASC
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*VocabEntry
	for rows.Next() {
		var entry VocabEntry
		err := rows.Scan(&entry.Word, &entry.Category, &entry.Normalized, &entry.Source, &entry.CreatedAt)
		if err != nil {
			return nil, err
		}
		entries = append(entries, &entry)
	}

	return entries, rows.Err()
}

// GetVocabData retrieves vocabulary grouped by category
// Returns data in the format: {"status": [...], "surface": [...], "flow": [...]}
func (s *PostgresVocabStorage) GetVocabData(ctx context.Context) (*VocabData, error) {
	query := `
		SELECT category, normalized
		FROM vocabulary
		ORDER BY category, normalized ASC
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	data := &VocabData{
		Status:  make([]string, 0),
		Surface: make([]string, 0),
		Flow:    make([]string, 0),
	}

	// Use maps to deduplicate normalized values
	statusSet := make(map[string]bool)
	surfaceSet := make(map[string]bool)
	flowSet := make(map[string]bool)

	for rows.Next() {
		var category, normalized string
		if err := rows.Scan(&category, &normalized); err != nil {
			return nil, err
		}

		switch VocabCategory(category) {
		case CategoryStatus:
			if !statusSet[normalized] {
				data.Status = append(data.Status, normalized)
				statusSet[normalized] = true
			}
		case CategorySurface:
			if !surfaceSet[normalized] {
				data.Surface = append(data.Surface, normalized)
				surfaceSet[normalized] = true
			}
		case CategoryFlow:
			if !flowSet[normalized] {
				data.Flow = append(data.Flow, normalized)
				flowSet[normalized] = true
			}
		}
	}

	return data, rows.Err()
}

// DeleteVocab removes a vocabulary entry
func (s *PostgresVocabStorage) DeleteVocab(ctx context.Context, word string) error {
	query := `DELETE FROM vocabulary WHERE word = $1`
	_, err := s.db.ExecContext(ctx, query, word)
	return err
}

// Close closes the database connection
func (s *PostgresVocabStorage) Close() error {
	return s.db.Close()
}

// GetVocabStats returns vocabulary storage statistics (implements VocabStats interface)
func (s *PostgresVocabStorage) GetVocabStats(ctx context.Context) (map[string]interface{}, error) {
	stats := make(map[string]interface{})
	stats["storage_type"] = "postgresql"

	// Total vocabulary entries
	var totalCount int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM vocabulary").Scan(&totalCount)
	if err != nil {
		return nil, err
	}
	stats["total_entries"] = totalCount

	// Count by category
	categoryQuery := `SELECT category, COUNT(*) FROM vocabulary GROUP BY category`
	rows, err := s.db.QueryContext(ctx, categoryQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	categoryCounts := make(map[string]int)
	for rows.Next() {
		var category string
		var count int
		if err := rows.Scan(&category, &count); err != nil {
			return nil, err
		}
		categoryCounts[category] = count
	}
	stats["by_category"] = categoryCounts

	// Count by source
	sourceQuery := `SELECT source, COUNT(*) FROM vocabulary GROUP BY source`
	rows2, err := s.db.QueryContext(ctx, sourceQuery)
	if err != nil {
		return nil, err
	}
	defer rows2.Close()

	sourceCounts := make(map[string]int)
	for rows2.Next() {
		var source string
		var count int
		if err := rows2.Scan(&source, &count); err != nil {
			return nil, err
		}
		sourceCounts[source] = count
	}
	stats["by_source"] = sourceCounts

	return stats, nil
}

// LookupWord implements the eventadapter.VocabLookup interface.
// Returns the category for a word, or empty string if not found.
func (s *PostgresVocabStorage) LookupWord(ctx context.Context, word string) (string, error) {
	entry, err := s.GetVocab(ctx, word)
	if err != nil {
		return "", err
	}
	if entry == nil {
		return "", nil
	}
	return string(entry.Category), nil
}
