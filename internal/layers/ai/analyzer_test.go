package ai

import (
	"testing"
	"time"

	"github.com/velum/internal/layers/baseline"
)

func TestAnalyzerName(t *testing.T) {
	analyzer := New()
	if analyzer.Name() != "ai_analyzer" {
		t.Errorf("Expected name 'ai_analyzer', got '%s'", analyzer.Name())
	}
}

func TestAnalyzerDisabled(t *testing.T) {
	// Test with AI disabled (default)
	analyzer := New()

	baselineResult := &baseline.BaselineResult{
		AnalyzedFlows:    map[string]interface{}{"test": "data"},
		DetectedPatterns: []string{"pattern1"},
		ChangeResults: []*baseline.ChangeResult{
			{
				PatternType:        "retry",
				Flow:               "payment",
				CurrentImpactRatio: 0.35,
				BaselineImpactRatio: 0.15,
				Delta:              0.20,
				DeltaPercentage:    1.33,
				Trend:              baseline.TrendIncreasing,
				ChangeSignificance: baseline.SignificanceHigh,
				BaselineStatus:     baseline.BaselineStatusSufficient,
				BaselineWindow:     "2026-01-07 to 2026-02-04",
				BaselineDays:       28,
			},
		},
		SnapshotDate: time.Now(),
	}

	result, err := analyzer.Process(baselineResult)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	aiResult, ok := result.(*AIResult)
	if !ok {
		t.Fatal("Expected *AIResult type")
	}

	if aiResult.AIEnabled {
		t.Error("Expected AIEnabled to be false when AI is disabled")
	}

	if aiResult.AIAnalysis != nil {
		t.Error("Expected AIAnalysis to be nil when AI is disabled")
	}

	// Verify original data is preserved
	if aiResult.AnalyzedFlows == nil {
		t.Error("Expected AnalyzedFlows to be preserved")
	}

	if len(aiResult.ChangeResults) != 1 {
		t.Errorf("Expected 1 change result, got %d", len(aiResult.ChangeResults))
	}
}

func TestAnalyzerWithConfig(t *testing.T) {
	config := &Config{
		Enabled: true,
		APIKey:  "", // Empty key should still disable actual API calls
		Model:   "llama-3.1-8b-instant",
	}

	analyzer := NewWithConfig(config)

	if analyzer.config.Enabled != true {
		t.Error("Expected Enabled to be true")
	}

	if analyzer.config.Model != "llama-3.1-8b-instant" {
		t.Errorf("Expected model 'llama-3.1-8b-instant', got '%s'", analyzer.config.Model)
	}
}

func TestAnalyzerPassThroughNonBaselineInput(t *testing.T) {
	analyzer := New()

	// Test with non-baseline input
	result, err := analyzer.Process("string input")
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	aiResult, ok := result.(*AIResult)
	if !ok {
		t.Fatal("Expected *AIResult type")
	}

	if aiResult.AIEnabled {
		t.Error("Expected AIEnabled to be false for non-baseline input")
	}
}

func TestBuildUserPrompt(t *testing.T) {
	analyzer := New()

	baselineResult := &baseline.BaselineResult{
		ChangeResults: []*baseline.ChangeResult{
			{
				PatternType:        "retry",
				Flow:               "payment",
				CurrentImpactRatio: 0.35,
				BaselineImpactRatio: 0.15,
				Delta:              0.20,
				DeltaPercentage:    1.33,
				Trend:              baseline.TrendIncreasing,
				ChangeSignificance: baseline.SignificanceHigh,
				BaselineStatus:     baseline.BaselineStatusSufficient,
				BaselineWindow:     "2026-01-07 to 2026-02-04",
				BaselineDays:       28,
			},
		},
	}

	prompt, err := analyzer.buildUserPrompt(baselineResult)
	if err != nil {
		t.Fatalf("buildUserPrompt failed: %v", err)
	}

	// Verify prompt contains expected information
	if len(prompt) == 0 {
		t.Error("Expected non-empty prompt")
	}

	if !contains(prompt, "retry") {
		t.Error("Expected prompt to contain pattern type 'retry'")
	}

	if !contains(prompt, "payment") {
		t.Error("Expected prompt to contain flow 'payment'")
	}
}

func TestParseAIResponse(t *testing.T) {
	analyzer := New()

	tests := []struct {
		name     string
		content  string
		wantErr  bool
		summary  string
	}{
		{
			name: "valid JSON",
			content: `{
				"summary": "Test summary",
				"details": ["Detail 1", "Detail 2"],
				"hypotheses": ["Hypothesis 1"],
				"confidence_note": "Test note"
			}`,
			wantErr: false,
			summary: "Test summary",
		},
		{
			name: "JSON with markdown code block",
			content: "```json\n{\"summary\": \"Test\", \"details\": [], \"hypotheses\": [], \"confidence_note\": \"Note\"}\n```",
			wantErr: false,
			summary: "Test",
		},
		{
			name:    "invalid JSON - fallback",
			content: "This is not JSON",
			wantErr: false, // Should not error, just return raw content
			summary: "Analysis complete",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := analyzer.parseAIResponse(tt.content)
			if tt.wantErr && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if result.Summary != tt.summary {
				t.Errorf("Expected summary '%s', got '%s'", tt.summary, result.Summary)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Enabled {
		t.Error("Expected default Enabled to be false")
	}

	if config.APIKey != "" {
		t.Error("Expected default APIKey to be empty")
	}

	if config.Model != "llama-3.1-8b-instant" {
		t.Errorf("Expected default model 'llama-3.1-8b-instant', got '%s'", config.Model)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
