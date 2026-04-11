package propertyagent

import (
	"context"
	"sync"
	"time"
)

// InMemoryPropertyStorage implements PropertyStorage for testing and demo use.
// No external dependencies required.
type InMemoryPropertyStorage struct {
	mu      sync.RWMutex
	entries map[string]*PropertyEntry
}

// NewInMemoryPropertyStorage creates a new in-memory property storage.
func NewInMemoryPropertyStorage() *InMemoryPropertyStorage {
	return &InMemoryPropertyStorage{
		entries: make(map[string]*PropertyEntry),
	}
}

// GetProperty retrieves a property entry by key name.
func (s *InMemoryPropertyStorage) GetProperty(ctx context.Context, keyName string) (*PropertyEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.entries[keyName]
	if !ok {
		return nil, nil
	}
	cp := *entry
	return &cp, nil
}

// GetPropertiesByRole retrieves all property entries for a specific role.
func (s *InMemoryPropertyStorage) GetPropertiesByRole(ctx context.Context, role PropertyRole) ([]*PropertyEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []*PropertyEntry
	for _, entry := range s.entries {
		if entry.Role == role {
			cp := *entry
			results = append(results, &cp)
		}
	}
	return results, nil
}

// GetAllProperties retrieves all property entries.
func (s *InMemoryPropertyStorage) GetAllProperties(ctx context.Context) ([]*PropertyEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	results := make([]*PropertyEntry, 0, len(s.entries))
	for _, entry := range s.entries {
		cp := *entry
		results = append(results, &cp)
	}
	return results, nil
}

// UpsertProperty stores or updates a single property entry.
func (s *InMemoryPropertyStorage) UpsertProperty(ctx context.Context, entry *PropertyEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	stored := *entry
	if stored.CreatedAt == "" {
		stored.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	s.entries[entry.KeyName] = &stored
	return nil
}

// UpsertPropertyBatch stores multiple property entries.
func (s *InMemoryPropertyStorage) UpsertPropertyBatch(ctx context.Context, entries []*PropertyEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().Format(time.RFC3339)
	for _, entry := range entries {
		stored := *entry
		if stored.CreatedAt == "" {
			stored.CreatedAt = now
		}
		s.entries[entry.KeyName] = &stored
	}
	return nil
}

// Close is a no-op for in-memory storage.
func (s *InMemoryPropertyStorage) Close() error { return nil }

// GetPropertyStats returns property storage statistics (implements PropertyStats).
func (s *InMemoryPropertyStorage) GetPropertyStats(ctx context.Context) (map[string]interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	roleCounts := make(map[string]int)
	sourceCounts := make(map[string]int)
	for _, entry := range s.entries {
		roleCounts[string(entry.Role)]++
		sourceCounts[entry.Source]++
	}

	return map[string]interface{}{
		"storage_type":  "in_memory",
		"total_entries": len(s.entries),
		"by_role":       roleCounts,
		"by_source":     sourceCounts,
	}, nil
}

// LookupProperty implements the eventadapter.PropertyLookup interface.
func (s *InMemoryPropertyStorage) LookupProperty(ctx context.Context, keyName string) (role string, label string, err error) {
	entry, err := s.GetProperty(ctx, keyName)
	if err != nil {
		return "", "", err
	}
	if entry == nil {
		return "", "", nil
	}
	return string(entry.Role), entry.Label, nil
}
