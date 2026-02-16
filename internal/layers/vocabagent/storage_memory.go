package vocabagent

import (
	"context"
	"sync"
	"time"
)

// InMemoryVocabStorage implements VocabStorage interface for testing purposes.
// It stores vocabulary entries in memory with no external dependencies.
type InMemoryVocabStorage struct {
	mu      sync.RWMutex
	entries map[string]*VocabEntry
}

// NewInMemoryVocabStorage creates a new in-memory vocabulary storage
func NewInMemoryVocabStorage() *InMemoryVocabStorage {
	return &InMemoryVocabStorage{
		entries: make(map[string]*VocabEntry),
	}
}

// UpsertVocab stores or updates a vocabulary entry
func (s *InMemoryVocabStorage) UpsertVocab(ctx context.Context, entry *VocabEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	stored := *entry
	if stored.CreatedAt == "" {
		stored.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	s.entries[entry.Word] = &stored
	return nil
}

// UpsertVocabBatch stores multiple vocabulary entries
func (s *InMemoryVocabStorage) UpsertVocabBatch(ctx context.Context, entries []*VocabEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().Format(time.RFC3339)
	for _, entry := range entries {
		stored := *entry
		if stored.CreatedAt == "" {
			stored.CreatedAt = now
		}
		s.entries[entry.Word] = &stored
	}
	return nil
}

// GetVocab retrieves a specific vocabulary entry by word
func (s *InMemoryVocabStorage) GetVocab(ctx context.Context, word string) (*VocabEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.entries[word]
	if !ok {
		return nil, nil
	}
	copy := *entry
	return &copy, nil
}

// GetVocabByCategory retrieves all vocabulary entries for a specific category
func (s *InMemoryVocabStorage) GetVocabByCategory(ctx context.Context, category VocabCategory) ([]*VocabEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []*VocabEntry
	for _, entry := range s.entries {
		if entry.Category == category {
			copy := *entry
			results = append(results, &copy)
		}
	}
	return results, nil
}

// GetAllVocab retrieves all vocabulary entries
func (s *InMemoryVocabStorage) GetAllVocab(ctx context.Context) ([]*VocabEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	results := make([]*VocabEntry, 0, len(s.entries))
	for _, entry := range s.entries {
		copy := *entry
		results = append(results, &copy)
	}
	return results, nil
}

// GetVocabData retrieves vocabulary grouped by category
func (s *InMemoryVocabStorage) GetVocabData(ctx context.Context) (*VocabData, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data := &VocabData{
		Status:  make([]string, 0),
		Surface: make([]string, 0),
		Flow:    make([]string, 0),
	}

	statusSet := make(map[string]bool)
	surfaceSet := make(map[string]bool)
	flowSet := make(map[string]bool)

	for _, entry := range s.entries {
		switch entry.Category {
		case CategoryStatus:
			if !statusSet[entry.Normalized] {
				data.Status = append(data.Status, entry.Normalized)
				statusSet[entry.Normalized] = true
			}
		case CategorySurface:
			if !surfaceSet[entry.Normalized] {
				data.Surface = append(data.Surface, entry.Normalized)
				surfaceSet[entry.Normalized] = true
			}
		case CategoryFlow:
			if !flowSet[entry.Normalized] {
				data.Flow = append(data.Flow, entry.Normalized)
				flowSet[entry.Normalized] = true
			}
		}
	}

	return data, nil
}

// DeleteVocab removes a vocabulary entry
func (s *InMemoryVocabStorage) DeleteVocab(ctx context.Context, word string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.entries, word)
	return nil
}

// Close is a no-op for in-memory storage
func (s *InMemoryVocabStorage) Close() error {
	return nil
}

// GetVocabStats returns vocabulary storage statistics (implements VocabStats interface)
func (s *InMemoryVocabStorage) GetVocabStats(ctx context.Context) (map[string]interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := make(map[string]interface{})
	stats["storage_type"] = "in_memory"
	stats["total_entries"] = len(s.entries)

	categoryCounts := make(map[string]int)
	sourceCounts := make(map[string]int)
	for _, entry := range s.entries {
		categoryCounts[string(entry.Category)]++
		sourceCounts[entry.Source]++
	}
	stats["by_category"] = categoryCounts
	stats["by_source"] = sourceCounts

	return stats, nil
}

// LookupWord implements the eventadapter.VocabLookup interface.
func (s *InMemoryVocabStorage) LookupWord(ctx context.Context, word string) (string, error) {
	entry, err := s.GetVocab(ctx, word)
	if err != nil {
		return "", err
	}
	if entry == nil {
		return "", nil
	}
	return string(entry.Category), nil
}
