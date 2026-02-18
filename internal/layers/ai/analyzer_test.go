package ai

import (
	"testing"
	"time"

	"github.com/velum/internal/canonical"
	"github.com/velum/internal/layers/baseline"
	"github.com/velum/internal/layers/behavior"
	"github.com/velum/internal/layers/pattern"
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
				PatternType:         "retry",
				Flow:                "payment",
				CurrentImpactRatio:  0.35,
				BaselineImpactRatio: 0.15,
				Delta:               0.20,
				DeltaPercentage:     1.33,
				Trend:               baseline.TrendIncreasing,
				ChangeSignificance:  baseline.SignificanceHigh,
				BaselineStatus:      baseline.BaselineStatusSufficient,
				BaselineWindow:      "2026-01-07 to 2026-02-04",
				BaselineDays:        28,
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

	// Verify change results are preserved
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
		DetectedPatterns: []*pattern.DetectedPattern{
			{
				Pattern:       pattern.PatternRetryStorm,
				Flow:          "payment",
				ContextKey:    "error_code=card_declined,plan_name=premium",
				AffectedUsers: 3,
				TotalFlows:    5,
				Severity:      pattern.SeverityHigh,
				Confidence:    pattern.ConfidenceHigh,
				Evidence: pattern.PatternEvidence{
					MatchingFlows: 3,
					Ratio:         0.60,
					Description:   "3 of 5 flows show retry behavior",
				},
			},
		},
		AnalyzedFlows: []*behavior.AnalyzedFlow{
			{UserID: "u1", Flow: "payment", Outcome: behavior.BehaviorRetry,
				Context: &canonical.EventContext{
					Dimensions: map[string]string{"device": "mobile", "country": "US"},
					Conditions: map[string]interface{}{"error_code": "card_declined"},
					Targets:    map[string]interface{}{"plan_name": "premium"},
				}},
			{UserID: "u2", Flow: "payment", Outcome: behavior.BehaviorRetry,
				Context: &canonical.EventContext{
					Dimensions: map[string]string{"device": "mobile", "country": "US"},
					Conditions: map[string]interface{}{"error_code": "card_declined"},
					Targets:    map[string]interface{}{"plan_name": "premium"},
				}},
			{UserID: "u3", Flow: "payment", Outcome: behavior.BehaviorSucceed,
				Context: &canonical.EventContext{
					Dimensions: map[string]string{"device": "desktop", "country": "UK"},
					Targets:    map[string]interface{}{"plan_name": "free"},
				}},
		},
		ChangeResults: []*baseline.ChangeResult{
			{
				PatternType:         "retry_storm",
				Flow:                "payment",
				ContextKey:          "error_code=card_declined,plan_name=premium",
				CurrentImpactRatio:  0.35,
				BaselineImpactRatio: 0.15,
				Delta:               0.20,
				DeltaPercentage:     1.33,
				Trend:               baseline.TrendIncreasing,
				ChangeSignificance:  baseline.SignificanceHigh,
				BaselineStatus:      baseline.BaselineStatusSufficient,
				BaselineWindow:      "2026-01-07 to 2026-02-04",
				BaselineDays:        28,
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

	if !contains(prompt, "retry_storm") {
		t.Error("Expected prompt to contain pattern type 'retry_storm'")
	}

	if !contains(prompt, "payment") {
		t.Error("Expected prompt to contain flow 'payment'")
	}

	if !contains(prompt, "error_code=card_declined,plan_name=premium") {
		t.Error("Expected prompt to contain context_key")
	}

	if !contains(prompt, "Context:") {
		t.Error("Expected prompt to contain 'Context:' label for context_key")
	}

	// Verify pattern evidence is included
	if !contains(prompt, "Severity: high") {
		t.Error("Expected prompt to contain severity")
	}

	if !contains(prompt, "Confidence: high") {
		t.Error("Expected prompt to contain confidence")
	}

	if !contains(prompt, "Affected Users: 3 / 5") {
		t.Error("Expected prompt to contain affected users count")
	}

	if !contains(prompt, "3 of 5 flows show retry behavior") {
		t.Error("Expected prompt to contain evidence description")
	}

	// Verify context breakdown is included
	if !contains(prompt, "Context Breakdown") {
		t.Error("Expected prompt to contain context breakdown section")
	}

	if !contains(prompt, "device:") {
		t.Error("Expected prompt to contain device breakdown")
	}

	if !contains(prompt, "mobile(2)") {
		t.Error("Expected prompt to contain mobile count")
	}

	if !contains(prompt, "desktop(1)") {
		t.Error("Expected prompt to contain desktop count")
	}
}

func TestBuildUserPromptWithoutContext(t *testing.T) {
	analyzer := New()

	baselineResult := &baseline.BaselineResult{
		DetectedPatterns: []*pattern.DetectedPattern{
			{
				Pattern:       pattern.PatternEarlyDropoff,
				Flow:          "registration",
				AffectedUsers: 4,
				TotalFlows:    4,
				Severity:      pattern.SeverityMedium,
				Confidence:    pattern.ConfidenceMedium,
				Evidence: pattern.PatternEvidence{
					Ratio: 1.0,
				},
			},
		},
		AnalyzedFlows: []*behavior.AnalyzedFlow{
			{UserID: "u1", Flow: "registration", Context: nil},
		},
		ChangeResults: []*baseline.ChangeResult{
			{
				PatternType:    "early_dropoff",
				Flow:           "registration",
				ContextKey:     "", // no context
				BaselineStatus: baseline.BaselineStatusFirstObservation,
			},
		},
	}

	prompt, err := analyzer.buildUserPrompt(baselineResult)
	if err != nil {
		t.Fatalf("buildUserPrompt failed: %v", err)
	}

	if contains(prompt, "Context:") {
		t.Error("Expected prompt to NOT contain 'Context:' label when context_key is empty")
	}

	if !contains(prompt, "early_dropoff") {
		t.Error("Expected prompt to contain pattern type")
	}

	if !contains(prompt, "Baseline Available: false") {
		t.Error("Expected prompt to indicate baseline not available for first observation")
	}

	// Pattern evidence should still be present
	if !contains(prompt, "Severity: medium") {
		t.Error("Expected prompt to contain severity even without context key")
	}

	if !contains(prompt, "Affected Users: 4 / 4") {
		t.Error("Expected prompt to contain affected users info")
	}
}

func TestParseAIResponse(t *testing.T) {
	analyzer := New()

	tests := []struct {
		name    string
		content string
		wantErr bool
		summary string
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
			name:    "JSON with markdown code block",
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
