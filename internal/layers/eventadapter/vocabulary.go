package eventadapter

import (
	_ "embed"
	"encoding/json"
	"log/slog"
	"sync"
)

// Vocabulary holds the controlled vocabulary for event normalization
type Vocabulary struct {
	Status  map[string]string // keyword -> normalized term
	Surface map[string]string // keyword -> normalized term
	Flow    map[string]string // keyword -> normalized term
	Noise   map[string]bool   // words to ignore
}

//go:embed vocabulary.json
var vocabularyJSON []byte

// vocabOnce ensures the builtin vocabulary is loaded exactly once.
var (
	vocabOnce    sync.Once
	builtinVocab *Vocabulary
)

// vocabFile is the JSON shape used for deserialization.
type vocabFile struct {
	Status  map[string]string `json:"status"`
	Surface map[string]string `json:"surface"`
	Flow    map[string]string `json:"flow"`
	Noise   []string          `json:"noise"`
}

// NewVocabulary returns the shared built-in vocabulary singleton loaded from
// the embedded vocabulary.json. The same pointer is returned on every call —
// do not mutate.
func NewVocabulary() *Vocabulary {
	vocabOnce.Do(func() {
		var f vocabFile
		if err := json.Unmarshal(vocabularyJSON, &f); err != nil {
			slog.Error("failed to load built-in vocabulary", "error", err)
			// Fall back to empty maps so the server can still start.
			builtinVocab = &Vocabulary{
				Status:  make(map[string]string),
				Surface: make(map[string]string),
				Flow:    make(map[string]string),
				Noise:   make(map[string]bool),
			}
			return
		}
		noise := make(map[string]bool, len(f.Noise))
		for _, w := range f.Noise {
			noise[w] = true
		}
		builtinVocab = &Vocabulary{
			Status:  f.Status,
			Surface: f.Surface,
			Flow:    f.Flow,
			Noise:   noise,
		}
	})
	return builtinVocab
}

// AddStatus adds a custom status keyword mapping
func (v *Vocabulary) AddStatus(keyword, normalized string) {
	v.Status[keyword] = normalized
}

// AddSurface adds a custom surface keyword mapping
func (v *Vocabulary) AddSurface(keyword, normalized string) {
	v.Surface[keyword] = normalized
}

// AddFlow adds a custom flow keyword mapping
func (v *Vocabulary) AddFlow(keyword, normalized string) {
	v.Flow[keyword] = normalized
}

// AddNoise adds a word to the noise list
func (v *Vocabulary) AddNoise(word string) {
	v.Noise[word] = true
}
