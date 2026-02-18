package canonical

import (
	"testing"
)

func TestNewEventContext(t *testing.T) {
	ctx := NewEventContext()

	if ctx == nil {
		t.Fatal("NewEventContext returned nil")
	}
	if ctx.Dimensions == nil {
		t.Fatal("Dimensions map not initialized")
	}
	if ctx.Targets == nil {
		t.Fatal("Targets map not initialized")
	}
	if ctx.Conditions == nil {
		t.Fatal("Conditions map not initialized")
	}
	if ctx.Measures == nil {
		t.Fatal("Measures map not initialized")
	}
	if !ctx.IsEmpty() {
		t.Fatal("New context should be empty")
	}
	if ctx.TotalProperties() != 0 {
		t.Fatal("New context should have 0 properties")
	}
}

func TestEventContext_IsEmpty(t *testing.T) {
	ctx := NewEventContext()

	if !ctx.IsEmpty() {
		t.Fatal("Empty context should return true")
	}

	ctx.Dimensions["device"] = "ios"
	if ctx.IsEmpty() {
		t.Fatal("Context with dimension should not be empty")
	}
}

func TestEventContext_TotalProperties(t *testing.T) {
	ctx := NewEventContext()
	ctx.Dimensions["device"] = "ios"
	ctx.Dimensions["country"] = "US"
	ctx.Targets["plan_name"] = "premium"
	ctx.Conditions["error_code"] = "card_declined"
	ctx.Measures["cart_value"] = 120.50

	if ctx.TotalProperties() != 5 {
		t.Fatalf("Expected 5 properties, got %d", ctx.TotalProperties())
	}
}

func TestEventContext_Merge(t *testing.T) {
	base := NewEventContext()
	base.Dimensions["device"] = "ios"
	base.Targets["plan_name"] = "free"
	base.Measures["cart_value"] = 50.0

	other := NewEventContext()
	other.Dimensions["device"] = "android" // Should NOT overwrite (first-seen wins)
	other.Dimensions["country"] = "US"     // Should be added
	other.Targets["plan_name"] = "premium" // Should overwrite (last value wins)
	other.Conditions["error_code"] = "card_declined"
	other.Measures["load_time"] = 340.0

	base.Merge(other)

	// Dimensions: first-seen wins
	if base.Dimensions["device"] != "ios" {
		t.Errorf("Dimension 'device' should be 'ios' (first-seen), got '%s'", base.Dimensions["device"])
	}
	if base.Dimensions["country"] != "US" {
		t.Errorf("Dimension 'country' should be 'US', got '%s'", base.Dimensions["country"])
	}

	// Targets: last value wins
	if base.Targets["plan_name"] != "premium" {
		t.Errorf("Target 'plan_name' should be 'premium' (last wins), got '%v'", base.Targets["plan_name"])
	}

	// Conditions: union
	if base.Conditions["error_code"] != "card_declined" {
		t.Errorf("Condition 'error_code' should be 'card_declined', got '%v'", base.Conditions["error_code"])
	}

	// Measures
	if base.Measures["cart_value"] != 50.0 {
		t.Errorf("Measure 'cart_value' should be 50.0, got %f", base.Measures["cart_value"])
	}
	if base.Measures["load_time"] != 340.0 {
		t.Errorf("Measure 'load_time' should be 340.0, got %f", base.Measures["load_time"])
	}

	if base.TotalProperties() != 6 {
		t.Errorf("Expected 6 total properties after merge, got %d", base.TotalProperties())
	}
}

func TestEventContext_MergeNil(t *testing.T) {
	ctx := NewEventContext()
	ctx.Dimensions["device"] = "ios"

	ctx.Merge(nil)

	if ctx.Dimensions["device"] != "ios" {
		t.Fatal("Merge nil should not modify context")
	}
}

func TestEventContext_Clone(t *testing.T) {
	original := NewEventContext()
	original.Dimensions["device"] = "ios"
	original.Targets["plan_name"] = "premium"
	original.Conditions["error_code"] = "card_declined"
	original.Measures["cart_value"] = 120.50

	clone := original.Clone()

	if clone.Dimensions["device"] != "ios" {
		t.Error("Clone dimensions mismatch")
	}
	if clone.Targets["plan_name"] != "premium" {
		t.Error("Clone targets mismatch")
	}

	// Verify deep copy
	clone.Dimensions["device"] = "android"
	if original.Dimensions["device"] != "ios" {
		t.Error("Clone is not a deep copy")
	}
}

func TestEventContext_CloneNil(t *testing.T) {
	var ctx *EventContext
	clone := ctx.Clone()
	if clone != nil {
		t.Fatal("Clone of nil should return nil")
	}
}

func TestIsCoreField(t *testing.T) {
	coreFields := []string{"id", "ts", "timestamp", "event", "event_name", "user_id", "session_id"}
	for _, f := range coreFields {
		if !IsCoreField(f) {
			t.Errorf("'%s' should be a core field", f)
		}
	}

	nonCoreFields := []string{"device", "plan_name", "error_code", "cart_value", "custom_field"}
	for _, f := range nonCoreFields {
		if IsCoreField(f) {
			t.Errorf("'%s' should NOT be a core field", f)
		}
	}
}

func TestIsDimension(t *testing.T) {
	tests := []struct {
		key           string
		expectedLabel string
		expectedFound bool
	}{
		{"device", "device", true},
		{"device_type", "device", true},
		{"deviceType", "device", true},
		{"country", "country", true},
		{"os", "platform", true},
		{"platform", "platform", true},
		{"browser", "browser", true},
		{"app_version", "app_version", true},
		{"utm_source", "utm_source", true},
		{"channel", "channel", true},
		{"environment", "environment", true},
		{"language", "language", true},
		{"lang", "language", true},
		{"plan_name", "", false},
		{"error_code", "", false},
		{"cart_value", "", false},
		{"custom_field", "", false},
	}

	for _, tt := range tests {
		label, found := IsDimension(tt.key)
		if found != tt.expectedFound {
			t.Errorf("IsDimension(%q): expected found=%v, got found=%v", tt.key, tt.expectedFound, found)
		}
		if label != tt.expectedLabel {
			t.Errorf("IsDimension(%q): expected label=%q, got label=%q", tt.key, tt.expectedLabel, label)
		}
	}
}

func TestIsMeasureValue(t *testing.T) {
	tests := []struct {
		value    interface{}
		expected bool
		numeric  float64
	}{
		{120.50, true, 120.50},
		{float32(42.0), true, 42.0},
		{100, true, 100.0},
		{int64(999), true, 999.0},
		{int32(50), true, 50.0},
		{uint(200), true, 200.0},
		{"premium", false, 0},
		{"card_declined", false, 0},
		{true, false, 0},
		{nil, false, 0},
		{[]string{"a"}, false, 0},
	}

	for _, tt := range tests {
		numeric, ok := IsMeasureValue(tt.value)
		if ok != tt.expected {
			t.Errorf("IsMeasureValue(%v): expected ok=%v, got ok=%v", tt.value, tt.expected, ok)
		}
		if ok && numeric != tt.numeric {
			t.Errorf("IsMeasureValue(%v): expected %f, got %f", tt.value, tt.numeric, numeric)
		}
	}
}

func TestFormatSampleValue(t *testing.T) {
	if FormatSampleValue(nil) != "" {
		t.Error("nil should format to empty string")
	}
	if FormatSampleValue("premium") != "premium" {
		t.Error("String should format as-is")
	}
	if FormatSampleValue(120.5) != "120.5" {
		t.Errorf("Float should format correctly, got %q", FormatSampleValue(120.5))
	}
	if FormatSampleValue(42) != "42" {
		t.Error("Int should format correctly")
	}
}

func TestPropertyRoleConstants(t *testing.T) {
	if RoleDimension != "dimension" {
		t.Error("RoleDimension should be 'dimension'")
	}
	if RoleTarget != "target" {
		t.Error("RoleTarget should be 'target'")
	}
	if RoleCondition != "condition" {
		t.Error("RoleCondition should be 'condition'")
	}
	if RoleMeasure != "measure" {
		t.Error("RoleMeasure should be 'measure'")
	}
}

func TestContextKey(t *testing.T) {
	if ContextKey != "_context" {
		t.Errorf("ContextKey should be '_context', got %q", ContextKey)
	}
}

func TestCheckRecommendedProperties(t *testing.T) {
	t.Run("no warnings when all recommended properties present", func(t *testing.T) {
		events := []map[string]interface{}{
			{"id": "1", "event": "click", "ts": 123, "device": "mobile", "country": "US", "platform": "ios"},
		}
		warnings := CheckRecommendedProperties(events)
		if len(warnings) != 0 {
			t.Errorf("expected 0 warnings, got %d: %v", len(warnings), warnings)
		}
	})

	t.Run("warnings for missing recommended properties", func(t *testing.T) {
		events := []map[string]interface{}{
			{"id": "1", "event": "click", "ts": 123},
		}
		warnings := CheckRecommendedProperties(events)
		if len(warnings) != 3 {
			t.Errorf("expected 3 warnings, got %d: %v", len(warnings), warnings)
		}
	})

	t.Run("accepts alternate field names", func(t *testing.T) {
		events := []map[string]interface{}{
			{"id": "1", "event": "click", "ts": 123, "device_type": "tablet", "region": "EU", "os": "android"},
		}
		warnings := CheckRecommendedProperties(events)
		if len(warnings) != 0 {
			t.Errorf("expected 0 warnings with alternate names, got %d: %v", len(warnings), warnings)
		}
	})

	t.Run("property in any event counts", func(t *testing.T) {
		events := []map[string]interface{}{
			{"id": "1", "event": "click", "ts": 123, "device": "mobile"},
			{"id": "2", "event": "view", "ts": 456, "country": "IN", "platform": "web"},
		}
		warnings := CheckRecommendedProperties(events)
		if len(warnings) != 0 {
			t.Errorf("expected 0 warnings (spread across events), got %d: %v", len(warnings), warnings)
		}
	})

	t.Run("empty events returns no warnings", func(t *testing.T) {
		warnings := CheckRecommendedProperties(nil)
		if warnings != nil {
			t.Errorf("expected nil for empty events, got %v", warnings)
		}
	})
}
