package propertyagent

import "context"

// PropertyStorage defines the interface for property registry persistence.
// Any storage backend (PostgreSQL, in-memory) must implement this.
type PropertyStorage interface {
	// GetProperty retrieves a property entry by key name.
	// Returns nil, nil if not found.
	GetProperty(ctx context.Context, keyName string) (*PropertyEntry, error)

	// GetPropertiesByRole retrieves all property entries for a specific role.
	GetPropertiesByRole(ctx context.Context, role PropertyRole) ([]*PropertyEntry, error)

	// GetAllProperties retrieves all property entries.
	GetAllProperties(ctx context.Context) ([]*PropertyEntry, error)

	// UpsertProperty stores or updates a single property entry.
	UpsertProperty(ctx context.Context, entry *PropertyEntry) error

	// UpsertPropertyBatch stores multiple property entries in a single transaction.
	UpsertPropertyBatch(ctx context.Context, entries []*PropertyEntry) error

	// Close closes the storage connection.
	Close() error
}

// PropertyStats provides optional statistics about property storage.
type PropertyStats interface {
	GetPropertyStats(ctx context.Context) (map[string]interface{}, error)
}
