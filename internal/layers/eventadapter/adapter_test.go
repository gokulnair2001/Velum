package eventadapter

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// --- Mock PropertyLookup for tests ---

type mockPropertyLookup struct {
	properties map[string]struct{ role, label string }
}

func newMockPropertyLookup() *mockPropertyLookup {
	return &mockPropertyLookup{
		properties: make(map[string]struct{ role, label string }),
	}
}

func (m *mockPropertyLookup) add(key, role, label string) {
	m.properties[key] = struct{ role, label string }{role, label}
}

func (m *mockPropertyLookup) LookupProperty(_ context.Context, keyName string) (string, string, error) {
	if entry, ok := m.properties[keyName]; ok {
		return entry.role, entry.label, nil
	}
	return "", "", nil
}

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
			expectedSurface: []string{},
			expectedFlow:    []string{"checkout", "payment"},
		},
		{
			input:           "login_modal_view",
			expectedStatus:  []string{"view"},
			expectedSurface: []string{"modal"},
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

// --- buildEventContext tests ---

func TestBuildEventContext_DimensionsAndMeasures(t *testing.T) {
	adapter := New() // no property lookup

	event := map[string]interface{}{
		"event":      "purchase_completed",
		"user_id":    "u123",
		"device":     "mobile",
		"country":    "US",
		"cart_value": 120.50,
		"load_time":  340,
	}

	ec := adapter.buildEventContext(event)
	if ec == nil {
		t.Fatal("Expected non-nil EventContext")
	}

	// Dimensions (built-in)
	if ec.Dimensions["device"] != "mobile" {
		t.Errorf("Dimensions[device] = %s, want mobile", ec.Dimensions["device"])
	}
	if ec.Dimensions["country"] != "US" {
		t.Errorf("Dimensions[country] = %s, want US", ec.Dimensions["country"])
	}

	// Measures (type-inferred)
	if ec.Measures["cart_value"] != 120.50 {
		t.Errorf("Measures[cart_value] = %f, want 120.50", ec.Measures["cart_value"])
	}
	if ec.Measures["load_time"] != 340 {
		t.Errorf("Measures[load_time] = %f, want 340", ec.Measures["load_time"])
	}

	// No targets/conditions without DB
	if len(ec.Targets) != 0 {
		t.Errorf("Expected no targets, got %d", len(ec.Targets))
	}
	if len(ec.Conditions) != 0 {
		t.Errorf("Expected no conditions, got %d", len(ec.Conditions))
	}
}

func TestBuildEventContext_PropertyLookup(t *testing.T) {
	lookup := newMockPropertyLookup()
	lookup.add("plan_name", "target", "plan_name")
	lookup.add("error_code", "condition", "error_code")

	adapter := NewWithLookups(nil, lookup)

	event := map[string]interface{}{
		"event":      "checkout_failed",
		"user_id":    "u456",
		"plan_name":  "premium",
		"error_code": "card_declined",
		"device":     "desktop",
		"amount":     49.99,
	}

	ec := adapter.buildEventContext(event)
	if ec == nil {
		t.Fatal("Expected non-nil EventContext")
	}

	// Target from DB
	if ec.Targets["plan_name"] != "premium" {
		t.Errorf("Targets[plan_name] = %v, want premium", ec.Targets["plan_name"])
	}

	// Condition from DB
	if ec.Conditions["error_code"] != "card_declined" {
		t.Errorf("Conditions[error_code] = %v, want card_declined", ec.Conditions["error_code"])
	}

	// Dimension (built-in, no DB needed)
	if ec.Dimensions["device"] != "desktop" {
		t.Errorf("Dimensions[device] = %s, want desktop", ec.Dimensions["device"])
	}

	// Measure (type-inferred, no DB needed)
	if ec.Measures["amount"] != 49.99 {
		t.Errorf("Measures[amount] = %f, want 49.99", ec.Measures["amount"])
	}
}

func TestBuildEventContext_CoreFieldsSkipped(t *testing.T) {
	adapter := New()

	// Event with only core fields — should produce nil context
	event := map[string]interface{}{
		"id":         "evt-1",
		"event":      "page_view",
		"user_id":    "u1",
		"session_id": "s1",
		"ts":         "2026-01-01T00:00:00Z",
	}

	ec := adapter.buildEventContext(event)
	if ec != nil {
		t.Errorf("Expected nil context for event with only core fields, got %+v", ec)
	}
}

func TestBuildEventContext_UnknownPropertySkipped(t *testing.T) {
	// Unknown string properties (not in DB, not dimension, not measure) are skipped
	adapter := New() // no property lookup

	event := map[string]interface{}{
		"event":        "test",
		"user_id":      "u1",
		"custom_field": "some_value", // unknown string — skipped (no DB)
		"device":       "mobile",     // dimension — included
	}

	ec := adapter.buildEventContext(event)
	if ec == nil {
		t.Fatal("Expected non-nil EventContext (device dimension)")
	}

	// Only device should be classified
	if ec.TotalProperties() != 1 {
		t.Errorf("Expected 1 property (device), got %d", ec.TotalProperties())
	}
	if ec.Dimensions["device"] != "mobile" {
		t.Errorf("Dimensions[device] = %s, want mobile", ec.Dimensions["device"])
	}
}

func TestBuildEventContext_DimensionVariants(t *testing.T) {
	adapter := New()

	event := map[string]interface{}{
		"event":       "test",
		"user_id":     "u1",
		"device_type": "tablet",
		"os":          "android",
		"lang":        "en",
		"env":         "production",
	}

	ec := adapter.buildEventContext(event)
	if ec == nil {
		t.Fatal("Expected non-nil EventContext")
	}

	// device_type → "device"
	if ec.Dimensions["device"] != "tablet" {
		t.Errorf("Dimensions[device] = %s, want tablet", ec.Dimensions["device"])
	}
	// os → "platform"
	if ec.Dimensions["platform"] != "android" {
		t.Errorf("Dimensions[platform] = %s, want android", ec.Dimensions["platform"])
	}
	// lang → "language"
	if ec.Dimensions["language"] != "en" {
		t.Errorf("Dimensions[language] = %s, want en", ec.Dimensions["language"])
	}
	// env → "environment"
	if ec.Dimensions["environment"] != "production" {
		t.Errorf("Dimensions[environment] = %s, want production", ec.Dimensions["environment"])
	}
}

func TestBuildEventContext_NilForEmptyEvent(t *testing.T) {
	adapter := New()

	event := map[string]interface{}{
		"event":   "test",
		"user_id": "u1",
	}

	ec := adapter.buildEventContext(event)
	if ec != nil {
		t.Errorf("Expected nil context for event with only core fields, got %+v", ec)
	}
}

func TestProcessEventMap_WithContext(t *testing.T) {
	lookup := newMockPropertyLookup()
	lookup.add("page_name", "target", "page_name")

	adapter := NewWithLookups(nil, lookup)

	input := map[string]interface{}{
		"event":     "page_view",
		"id":        "evt-1",
		"ts":        "2026-01-01T00:00:00Z",
		"user_id":   "u1",
		"page_name": "pricing",
		"device":    "mobile",
		"load_time": 250,
	}

	result, err := adapter.Process(input)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	processed, ok := result.(*ProcessedEvent)
	if !ok {
		t.Fatalf("Expected *ProcessedEvent, got %T", result)
	}

	// Context should be built during processing
	if processed.Context == nil {
		t.Fatal("Expected non-nil Context on ProcessedEvent")
	}

	if processed.Context.Targets["page_name"] != "pricing" {
		t.Errorf("Context.Targets[page_name] = %v, want pricing", processed.Context.Targets["page_name"])
	}
	if processed.Context.Dimensions["device"] != "mobile" {
		t.Errorf("Context.Dimensions[device] = %s, want mobile", processed.Context.Dimensions["device"])
	}
	if processed.Context.Measures["load_time"] != 250 {
		t.Errorf("Context.Measures[load_time] = %f, want 250", processed.Context.Measures["load_time"])
	}
}
