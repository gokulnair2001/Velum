package propertyagent

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"
	"github.com/velum/internal/config"
)

// PostgresPropertyStorage implements PropertyStorage with PostgreSQL persistence.
// It stores the property registry in a "property_registry" table.
type PostgresPropertyStorage struct {
	db      *sql.DB
	connStr string
}

// NewPostgresPropertyStorage creates a new PostgreSQL-backed property storage.
func NewPostgresPropertyStorage(cfg *config.PostgresStorageConfig) (*PostgresPropertyStorage, error) {
	if cfg == nil {
		return nil, fmt.Errorf("postgres config is nil")
	}

	connStr := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Host,
		cfg.Port,
		cfg.User,
		cfg.Password,
		cfg.Database,
		cfg.SSLMode,
	)

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	maxConns := cfg.MaxConnections
	if maxConns == 0 {
		maxConns = 10
	}
	db.SetMaxOpenConns(maxConns)
	db.SetMaxIdleConns(maxConns / 2)
	db.SetConnMaxLifetime(time.Hour)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	storage := &PostgresPropertyStorage{
		db:      db,
		connStr: connStr,
	}

	if err := storage.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize property registry schema: %w", err)
	}

	return storage, nil
}

// initSchema creates the property_registry table if it doesn't exist.
func (s *PostgresPropertyStorage) initSchema() error {
	schema := `
		CREATE TABLE IF NOT EXISTS property_registry (
			key_name TEXT PRIMARY KEY NOT NULL,
			role TEXT NOT NULL CHECK(role IN ('dimension', 'target', 'condition', 'measure')),
			label TEXT NOT NULL DEFAULT '',
			sample_value TEXT NOT NULL DEFAULT '',
			source TEXT NOT NULL DEFAULT 'ai',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE INDEX IF NOT EXISTS idx_property_registry_role
		ON property_registry (role);
	`

	_, err := s.db.Exec(schema)
	return err
}

// GetProperty retrieves a property entry by key name.
// Returns nil, nil if not found.
func (s *PostgresPropertyStorage) GetProperty(ctx context.Context, keyName string) (*PropertyEntry, error) {
	query := `
		SELECT key_name, role, label, sample_value, source, created_at
		FROM property_registry
		WHERE key_name = $1
	`

	var entry PropertyEntry
	err := s.db.QueryRowContext(ctx, query, keyName).Scan(
		&entry.KeyName,
		&entry.Role,
		&entry.Label,
		&entry.SampleValue,
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

// GetPropertiesByRole retrieves all property entries for a specific role.
func (s *PostgresPropertyStorage) GetPropertiesByRole(ctx context.Context, role PropertyRole) ([]*PropertyEntry, error) {
	query := `
		SELECT key_name, role, label, sample_value, source, created_at
		FROM property_registry
		WHERE role = $1
		ORDER BY key_name ASC
	`

	rows, err := s.db.QueryContext(ctx, query, role)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*PropertyEntry
	for rows.Next() {
		var entry PropertyEntry
		err := rows.Scan(&entry.KeyName, &entry.Role, &entry.Label, &entry.SampleValue, &entry.Source, &entry.CreatedAt)
		if err != nil {
			return nil, err
		}
		entries = append(entries, &entry)
	}

	return entries, rows.Err()
}

// GetAllProperties retrieves all property entries.
func (s *PostgresPropertyStorage) GetAllProperties(ctx context.Context) ([]*PropertyEntry, error) {
	query := `
		SELECT key_name, role, label, sample_value, source, created_at
		FROM property_registry
		ORDER BY role, key_name ASC
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*PropertyEntry
	for rows.Next() {
		var entry PropertyEntry
		err := rows.Scan(&entry.KeyName, &entry.Role, &entry.Label, &entry.SampleValue, &entry.Source, &entry.CreatedAt)
		if err != nil {
			return nil, err
		}
		entries = append(entries, &entry)
	}

	return entries, rows.Err()
}

// UpsertProperty stores or updates a single property entry.
func (s *PostgresPropertyStorage) UpsertProperty(ctx context.Context, entry *PropertyEntry) error {
	query := `
		INSERT INTO property_registry (key_name, role, label, sample_value, source, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT(key_name)
		DO UPDATE SET
			role = EXCLUDED.role,
			label = EXCLUDED.label,
			sample_value = EXCLUDED.sample_value,
			source = EXCLUDED.source
	`

	createdAt := entry.CreatedAt
	if createdAt == "" {
		createdAt = time.Now().UTC().Format(time.RFC3339)
	}

	_, err := s.db.ExecContext(ctx, query,
		entry.KeyName,
		entry.Role,
		entry.Label,
		entry.SampleValue,
		entry.Source,
		createdAt,
	)

	return err
}

// UpsertPropertyBatch stores multiple property entries in a single transaction.
func (s *PostgresPropertyStorage) UpsertPropertyBatch(ctx context.Context, entries []*PropertyEntry) error {
	if len(entries) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	query := `
		INSERT INTO property_registry (key_name, role, label, sample_value, source, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT(key_name)
		DO UPDATE SET
			role = EXCLUDED.role,
			label = EXCLUDED.label,
			sample_value = EXCLUDED.sample_value,
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

		_, err := stmt.ExecContext(ctx, entry.KeyName, entry.Role, entry.Label, entry.SampleValue, entry.Source, createdAt)
		if err != nil {
			return fmt.Errorf("failed to upsert property '%s': %w", entry.KeyName, err)
		}
	}

	return tx.Commit()
}

// Close closes the database connection.
func (s *PostgresPropertyStorage) Close() error {
	return s.db.Close()
}

// GetPropertyStats returns property storage statistics.
func (s *PostgresPropertyStorage) GetPropertyStats(ctx context.Context) (map[string]interface{}, error) {
	stats := make(map[string]interface{})
	stats["storage_type"] = "postgresql"

	// Total count
	var totalCount int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM property_registry").Scan(&totalCount)
	if err != nil {
		return nil, err
	}
	stats["total_entries"] = totalCount

	// Count by role
	roleQuery := `SELECT role, COUNT(*) FROM property_registry GROUP BY role`
	rows, err := s.db.QueryContext(ctx, roleQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	roleCounts := make(map[string]int)
	for rows.Next() {
		var role string
		var count int
		if err := rows.Scan(&role, &count); err != nil {
			return nil, err
		}
		roleCounts[role] = count
	}
	stats["by_role"] = roleCounts

	// Count by source
	sourceQuery := `SELECT source, COUNT(*) FROM property_registry GROUP BY source`
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

// LookupProperty implements the eventadapter.PropertyLookup interface.
// Returns the role and label for a property key, or empty strings if not found.
func (s *PostgresPropertyStorage) LookupProperty(ctx context.Context, keyName string) (string, string, error) {
	entry, err := s.GetProperty(ctx, keyName)
	if err != nil {
		return "", "", err
	}
	if entry == nil {
		return "", "", nil
	}
	return string(entry.Role), entry.Label, nil
}
