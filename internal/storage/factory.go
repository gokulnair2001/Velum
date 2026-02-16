package storage

import (
	"fmt"

	"github.com/velum/internal/config"
)

// NewStorage creates a storage instance based on configuration
// This is the factory that selects the appropriate storage backend
func NewStorage(cfg *config.StorageConfig) (Storage, error) {
	if cfg == nil {
		return nil, fmt.Errorf("storage config is nil")
	}

	// Default to postgres if type is not specified
	storageType := cfg.Type
	if storageType == "" {
		storageType = "postgres"
	}

	switch storageType {
	case "postgres", "postgresql":
		return newPostgresFromConfig(cfg)

	default:
		return nil, fmt.Errorf("unsupported storage type: %s (supported: postgres)", storageType)
	}
}

// newPostgresFromConfig creates PostgreSQL storage from config
func newPostgresFromConfig(cfg *config.StorageConfig) (Storage, error) {
	// Validate required fields
	if cfg.Postgres.Host == "" {
		return nil, fmt.Errorf("postgres.host is required")
	}
	if cfg.Postgres.Database == "" {
		return nil, fmt.Errorf("postgres.database is required")
	}
	if cfg.Postgres.User == "" {
		return nil, fmt.Errorf("postgres.user is required")
	}
	if cfg.Postgres.Password == "" {
		return nil, fmt.Errorf("postgres.password is required")
	}

	// Apply defaults for optional fields
	if cfg.Postgres.Port == 0 {
		cfg.Postgres.Port = 5432
	}
	if cfg.Postgres.SSLMode == "" {
		cfg.Postgres.SSLMode = "prefer"
	}
	if cfg.Postgres.MaxConnections == 0 {
		cfg.Postgres.MaxConnections = 25
	}

	return NewPostgresStorage(&cfg.Postgres, cfg.RetentionDays)
}
