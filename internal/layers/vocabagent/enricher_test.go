package vocabagent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestVocabEnricherName(t *testing.T) {
	enricher := NewVocabEnricher(nil, nil, false)
	if enricher.Name() != "vocab_enricher" {
		t.Errorf("Expected name 'vocab_enricher', got '%s'", enricher.Name())
	}
}

func TestVocabEnricherDisabledWithoutStorage(t *testing.T) {
	agent := New()
	enricher := NewVocabEnricher(nil, agent, false)

	input := []map[string]interface{}{
		{"event": "button_click", "id": "1", "ts": "2026-02-09T10:00:00Z"},
	}

	result, err := enricher.Process(input)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Should pass through unchanged
	resultEvents, ok := result.([]map[string]interface{})
	if !ok {
		t.Fatal("Expected []map[string]interface{} type")
	}

	if len(resultEvents) != 1 {
		t.Errorf("Expected 1 event, got %d", len(resultEvents))
	}
}

func TestVocabEnricherDisabledWithoutAgent(t *testing.T) {
	// Create temp storage
	tmpDir, err := os.MkdirTemp("", "enricher_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	storage, _ := NewSQLiteVocabStorage(&SQLiteVocabConfig{
		DBPath: filepath.Join(tmpDir, "test.db"),
	})
	defer storage.Close()

	enricher := NewVocabEnricher(storage, nil, false)

	input := "test_event"
	result, err := enricher.Process(input)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Should pass through unchanged
	if result != input {
		t.Error("Expected input to pass through unchanged")
	}
}

func TestVocabEnricherTokenize(t *testing.T) {
	enricher := NewVocabEnricher(nil, nil, false)

	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "underscore separated",
			input:    "button_click_success",
			expected: []string{"button", "click", "success"},
		},
		{
			name:     "camelCase",
			input:    "buttonClickSuccess",
			expected: []string{"button", "click", "success"},
		},
		{
			name:     "PascalCase",
			input:    "ButtonClickSuccess",
			expected: []string{"button", "click", "success"},
		},
		{
			name:     "mixed delimiters",
			input:    "user-profile.view:modal",
			expected: []string{"user", "profile", "view", "modal"},
		},
		{
			name:     "with numbers",
			input:    "page1_view2",
			expected: []string{"page1", "view2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := enricher.tokenize(tt.input)
			if len(tokens) != len(tt.expected) {
				t.Errorf("Expected %d tokens, got %d: %v", len(tt.expected), len(tokens), tokens)
				return
			}
			for i, exp := range tt.expected {
				if tokens[i] != exp {
					t.Errorf("Token %d: expected '%s', got '%s'", i, exp, tokens[i])
				}
			}
		})
	}
}

func TestVocabEnricherExtractTokens(t *testing.T) {
	enricher := NewVocabEnricher(nil, nil, false)

	tests := []struct {
		name        string
		input       interface{}
		minExpected int
	}{
		{
			name:        "string input",
			input:       "button_click_success",
			minExpected: 3,
		},
		{
			name:        "string slice",
			input:       []string{"modal_view", "form_submit"},
			minExpected: 4,
		},
		{
			name: "event map",
			input: map[string]interface{}{
				"event": "checkout_started",
				"id":    "123",
			},
			minExpected: 2,
		},
		{
			name: "event maps slice",
			input: []map[string]interface{}{
				{"event": "login_success"},
				{"event": "profile_view"},
			},
			minExpected: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := enricher.extractTokens(tt.input)
			if len(tokens) < tt.minExpected {
				t.Errorf("Expected at least %d tokens, got %d: %v", tt.minExpected, len(tokens), tokens)
			}
		})
	}
}

func TestVocabEnricherFindUnknownTokens(t *testing.T) {
	// Create temp storage with some known vocab
	tmpDir, err := os.MkdirTemp("", "enricher_unknown_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	storage, err := NewSQLiteVocabStorage(&SQLiteVocabConfig{
		DBPath: filepath.Join(tmpDir, "test.db"),
	})
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()

	// Add some known words
	storage.UpsertVocab(ctx, &VocabEntry{Word: "click", Category: CategoryStatus, Normalized: "click", Source: "builtin"})
	storage.UpsertVocab(ctx, &VocabEntry{Word: "button", Category: CategorySurface, Normalized: "button", Source: "builtin"})

	enricher := NewVocabEnricher(storage, nil, false)

	// Test finding unknown tokens
	tokens := []string{"click", "button", "customword", "anotherterm"}
	unknown, err := enricher.findUnknownTokens(ctx, tokens)
	if err != nil {
		t.Fatalf("findUnknownTokens failed: %v", err)
	}

	if len(unknown) != 2 {
		t.Errorf("Expected 2 unknown tokens, got %d: %v", len(unknown), unknown)
	}

	// Check that known words are not in unknown list
	for _, u := range unknown {
		if u == "click" || u == "button" {
			t.Errorf("Known word '%s' should not be in unknown list", u)
		}
	}
}

func TestVocabEnricherStoreClassifiedWords(t *testing.T) {
	// Create temp storage
	tmpDir, err := os.MkdirTemp("", "enricher_store_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	storage, err := NewSQLiteVocabStorage(&SQLiteVocabConfig{
		DBPath: filepath.Join(tmpDir, "test.db"),
	})
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()
	enricher := NewVocabEnricher(storage, nil, false)

	// Test storing classified words
	classified := &VocabData{
		Status:  []string{"customstatus"},
		Surface: []string{"customsurface"},
		Flow:    []string{"customflow"},
	}

	count, err := enricher.storeClassifiedWords(ctx, classified)
	if err != nil {
		t.Fatalf("storeClassifiedWords failed: %v", err)
	}

	if count != 3 {
		t.Errorf("Expected 3 stored words, got %d", count)
	}

	// Verify they're in storage
	entry, _ := storage.GetVocab(ctx, "customstatus")
	if entry == nil {
		t.Error("Expected 'customstatus' to be stored")
	} else {
		if entry.Category != CategoryStatus {
			t.Errorf("Expected category 'status', got '%s'", entry.Category)
		}
		if entry.Source != "ai" {
			t.Errorf("Expected source 'ai', got '%s'", entry.Source)
		}
	}
}

func TestIsNumeric(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"123", true},
		{"12345", true},
		{"abc", false},
		{"12a3", false},
		{"", true}, // empty string has no non-digits
	}

	for _, tt := range tests {
		result := isNumeric(tt.input)
		if result != tt.expected {
			t.Errorf("isNumeric(%s): expected %v, got %v", tt.input, tt.expected, result)
		}
	}
}
