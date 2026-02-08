package vocabagent

import "context"

// VocabCategory represents the category of a vocabulary word
type VocabCategory string

const (
	CategoryStatus  VocabCategory = "status"
	CategorySurface VocabCategory = "surface"
	CategoryFlow    VocabCategory = "flow"
)

// VocabEntry represents a single vocabulary mapping
type VocabEntry struct {
	Word       string        `json:"word"`        // The original word (lowercase)
	Category   VocabCategory `json:"category"`    // status, surface, or flow
	Normalized string        `json:"normalized"`  // The normalized term
	Source     string        `json:"source"`      // "ai" for AI-generated, "builtin" for defaults
	CreatedAt  string        `json:"created_at"`  // ISO timestamp
}

// VocabData represents vocabulary data grouped by category
// Matches the format: {"status": [...], "surface": [...], "flow": [...]}
type VocabData struct {
	Status  []string `json:"status"`
	Surface []string `json:"surface"`
	Flow    []string `json:"flow"`
}

// VocabStorage defines the interface for vocabulary persistence
type VocabStorage interface {
	// UpsertVocab stores or updates a vocabulary entry
	// Idempotent - safe for retries
	UpsertVocab(ctx context.Context, entry *VocabEntry) error

	// UpsertVocabBatch stores multiple vocabulary entries in a single transaction
	UpsertVocabBatch(ctx context.Context, entries []*VocabEntry) error

	// GetVocab retrieves a specific vocabulary entry by word
	GetVocab(ctx context.Context, word string) (*VocabEntry, error)

	// GetVocabByCategory retrieves all vocabulary entries for a specific category
	GetVocabByCategory(ctx context.Context, category VocabCategory) ([]*VocabEntry, error)

	// GetAllVocab retrieves all vocabulary entries
	GetAllVocab(ctx context.Context) ([]*VocabEntry, error)

	// GetVocabData retrieves vocabulary grouped by category (for the eventadapter)
	GetVocabData(ctx context.Context) (*VocabData, error)

	// DeleteVocab removes a vocabulary entry
	DeleteVocab(ctx context.Context, word string) error

	// Close closes the storage connection
	Close() error
}

// VocabStats provides optional statistics about vocabulary storage
type VocabStats interface {
	GetVocabStats(ctx context.Context) (map[string]interface{}, error)
}
