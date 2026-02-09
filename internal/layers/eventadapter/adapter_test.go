package eventadapter

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTokenize(t *testing.T) {
	adapter := New()

	tests := []struct {
		input    string
		expected []string
	}{
		{"button_click_success", []string{"button", "click", "success"}},
		{"home-page-view", []string{"home", "page", "view"}},
		{"userLoginSuccess", []string{"user", "login", "success"}},
		{"checkout.payment.failed", []string{"checkout", "payment", "failed"}},
		{"nav_sidebar_click", []string{"nav", "sidebar", "click"}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := adapter.tokenize(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("tokenize(%s) = %v, want %v", tt.input, result, tt.expected)
				return
			}
			for i, tok := range result {
				if tok != tt.expected[i] {
					t.Errorf("tokenize(%s)[%d] = %s, want %s", tt.input, i, tok, tt.expected[i])
				}
			}
		})
	}
}

func TestNormalizeEventString(t *testing.T) {
	adapter := New()

	tests := []struct {
		input           string
		expectedStatus  []string
		expectedSurface []string
		expectedFlow    []string
	}{
		{
			input:           "button_click_success",
			expectedStatus:  []string{"click", "success"},
			expectedSurface: []string{"button"},
			expectedFlow:    []string{},
		},
		{
			input:           "checkout_payment_failed",
			expectedStatus:  []string{"failed"},
			expectedSurface: []string{"checkout"},
			expectedFlow:    []string{"checkout", "payment"},
		},
		{
			input:           "login_modal_view",
			expectedStatus:  []string{"view"},
			expectedSurface: []string{"modal", "view"},
			expectedFlow:    []string{"authentication"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := adapter.NormalizeEventString(tt.input)

			if !sliceEqual(result.Status, tt.expectedStatus) {
				t.Errorf("Status = %v, want %v", result.Status, tt.expectedStatus)
			}
			if !sliceEqual(result.Surface, tt.expectedSurface) {
				t.Errorf("Surface = %v, want %v", result.Surface, tt.expectedSurface)
			}
			if !sliceEqual(result.Flow, tt.expectedFlow) {
				t.Errorf("Flow = %v, want %v", result.Flow, tt.expectedFlow)
			}
		})
	}
}

func TestProcessEventMap(t *testing.T) {
	adapter := New()

	input := map[string]interface{}{
		"event":   "pay_click",
		"id":      "u1",
		"user_id": 222,
		"ts":      "2026-02-04T10:12:01Z",
	}

	result, err := adapter.Process(input)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	processed, ok := result.(*ProcessedEvent)
	if !ok {
		t.Fatalf("Expected *ProcessedEvent, got %T", result)
	}

	// Check normalized event
	if processed.Normalized.Original != "pay_click" {
		t.Errorf("Original = %v, want pay_click", processed.Normalized.Original)
	}

	// Check that original fields are preserved
	if processed.OriginalFields["id"] != "u1" {
		t.Errorf("id = %v, want u1", processed.OriginalFields["id"])
	}
	if processed.OriginalFields["user_id"] != 222 {
		t.Errorf("user_id = %v, want 222", processed.OriginalFields["user_id"])
	}
	if processed.OriginalFields["ts"] != "2026-02-04T10:12:01Z" {
		t.Errorf("ts = %v, want 2026-02-04T10:12:01Z", processed.OriginalFields["ts"])
	}

	// Check JSON output includes all fields
	jsonBytes, err := json.Marshal(processed)
	if err != nil {
		t.Fatalf("JSON marshal failed: %v", err)
	}

	var jsonMap map[string]interface{}
	json.Unmarshal(jsonBytes, &jsonMap)

	if jsonMap["id"] != "u1" {
		t.Errorf("JSON id = %v, want u1", jsonMap["id"])
	}
	if jsonMap["normalized"] == nil {
		t.Error("JSON normalized is nil")
	}
}

func TestValidation(t *testing.T) {
	adapter := New()

	tests := []struct {
		name     string
		input    map[string]interface{}
		expected bool
	}{
		{
			name: "valid event with all required fields",
			input: map[string]interface{}{
				"event": "pay_click",
				"id":    "evt_123",
				"ts":    "2026-02-04T10:12:01Z",
			},
			expected: true,
		},
		{
			name: "missing id field",
			input: map[string]interface{}{
				"event": "pay_click",
				"ts":    "2026-02-04T10:12:01Z",
			},
			expected: false,
		},
		{
			name: "missing ts field",
			input: map[string]interface{}{
				"event": "pay_click",
				"id":    "evt_123",
			},
			expected: false,
		},
		{
			name: "missing both id and ts",
			input: map[string]interface{}{
				"event": "pay_click",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := adapter.isValidEvent(tt.input)
			if result != tt.expected {
				t.Errorf("isValidEvent() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestProcessRejectsInvalidEvents(t *testing.T) {
	adapter := New()

	tests := []struct {
		name        string
		input       []map[string]interface{}
		expectError bool
		errContains string
	}{
		{
			name: "all valid events",
			input: []map[string]interface{}{
				{"event": "valid1", "id": "evt_1", "ts": "2026-02-04T10:12:01Z"},
				{"event": "valid2", "id": "evt_2", "ts": "2026-02-04T10:12:02Z"},
			},
			expectError: false,
		},
		{
			name: "event missing id",
			input: []map[string]interface{}{
				{"event": "valid1", "id": "evt_1", "ts": "2026-02-04T10:12:01Z"},
				{"event": "missing_id", "ts": "2026-02-04T10:12:02Z"},
			},
			expectError: true,
			errContains: "missing mandatory field: id",
		},
		{
			name: "event missing ts",
			input: []map[string]interface{}{
				{"event": "missing_ts", "id": "evt_1"},
			},
			expectError: true,
			errContains: "missing mandatory field: ts",
		},
		{
			name: "event missing both",
			input: []map[string]interface{}{
				{"event": "missing_both"},
			},
			expectError: true,
			errContains: "missing mandatory fields: id, ts",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := adapter.Process(tt.input)

			if tt.expectError {
				if err == nil {
					t.Error("Expected an error, got nil")
					return
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("Error message should contain '%s', got '%s'", tt.errContains, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}
				processed, ok := result.([]*ProcessedEvent)
				if !ok {
					t.Fatalf("Expected []*ProcessedEvent, got %T", result)
				}
				if len(processed) != len(tt.input) {
					t.Errorf("Expected %d events, got %d", len(tt.input), len(processed))
				}
			}
		})
	}
}

func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
