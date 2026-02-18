package canonical

// ContextKey is the key used to attach canonical context to event maps.
// All layers reference this constant instead of hardcoding the key name.
const ContextKey = "_context"

// PropertyRole represents the role a property plays in behavioral analysis.
// Properties are classified into one of four roles, each resolved differently.
type PropertyRole string

const (
	// RoleDimension represents fixed slicing dimensions (device, country, platform).
	// Resolved by built-in known list. No AI needed.
	RoleDimension PropertyRole = "dimension"

	// RoleTarget represents what the action is about (plan_name, product_id).
	// Resolved by AI classification when not in the property registry.
	RoleTarget PropertyRole = "target"

	// RoleCondition represents circumstances under which the action occurred
	// (error_code, ab_variant). Resolved by AI classification.
	RoleCondition PropertyRole = "condition"

	// RoleMeasure represents quantitative values (cart_value, load_time_ms).
	// Resolved by type inference. Numeric values are measures.
	RoleMeasure PropertyRole = "measure"
)

// EventContext represents the classified canonical context attached to an event.
// It is populated by the Context Enricher layer (Layer 0) and flows through
// the entire pipeline, giving every downstream layer access to structured
// contextual information about the event.
type EventContext struct {
	// Dimensions are standard analytics slicing fields (device, country, platform).
	// Low cardinality, well-known. Extracted by built-in list match.
	// Key = normalized label, Value = raw value from event.
	Dimensions map[string]string `json:"dimensions,omitempty"`

	// Targets identify what the action is about (plan_name=premium, product_id=xyz).
	// Domain-specific object references. Classified by AI.
	// Key = original field name, Value = raw value from event.
	Targets map[string]interface{} `json:"targets,omitempty"`

	// Conditions describe circumstances (error_code=card_declined, ab_variant=B).
	// Causal/explanatory context. Classified by AI.
	// Key = original field name, Value = raw value from event.
	Conditions map[string]interface{} `json:"conditions,omitempty"`

	// Measures are quantitative values (cart_value=120.50, load_time_ms=340).
	// Inferred by type check (numeric values).
	// Key = original field name, Value = numeric value.
	Measures map[string]float64 `json:"measures,omitempty"`
}

// NewEventContext creates an empty EventContext with initialized maps.
func NewEventContext() *EventContext {
	return &EventContext{
		Dimensions: make(map[string]string),
		Targets:    make(map[string]interface{}),
		Conditions: make(map[string]interface{}),
		Measures:   make(map[string]float64),
	}
}

// IsEmpty returns true if the context has no classified properties.
func (ec *EventContext) IsEmpty() bool {
	return len(ec.Dimensions) == 0 &&
		len(ec.Targets) == 0 &&
		len(ec.Conditions) == 0 &&
		len(ec.Measures) == 0
}

// TotalProperties returns the total number of classified properties across all roles.
func (ec *EventContext) TotalProperties() int {
	return len(ec.Dimensions) + len(ec.Targets) + len(ec.Conditions) + len(ec.Measures)
}

// Merge combines another EventContext into this one.
// Merge strategy:
//   - Dimensions: first-seen value wins (dimensions should be consistent within a flow)
//   - Targets: last value wins per key (union across events)
//   - Conditions: last value wins per key (union across events)
//   - Measures: last value wins per key
func (ec *EventContext) Merge(other *EventContext) {
	if other == nil {
		return
	}

	for k, v := range other.Dimensions {
		if _, exists := ec.Dimensions[k]; !exists {
			ec.Dimensions[k] = v
		}
	}

	for k, v := range other.Targets {
		ec.Targets[k] = v
	}

	for k, v := range other.Conditions {
		ec.Conditions[k] = v
	}

	for k, v := range other.Measures {
		ec.Measures[k] = v
	}
}

// Clone creates a deep copy of the EventContext.
func (ec *EventContext) Clone() *EventContext {
	if ec == nil {
		return nil
	}

	clone := NewEventContext()

	for k, v := range ec.Dimensions {
		clone.Dimensions[k] = v
	}
	for k, v := range ec.Targets {
		clone.Targets[k] = v
	}
	for k, v := range ec.Conditions {
		clone.Conditions[k] = v
	}
	for k, v := range ec.Measures {
		clone.Measures[k] = v
	}

	return clone
}

// PropertyEntry represents a classified property in the registry.
// Stored in the PostgreSQL property_registry table.
// Once a property key is classified and stored, it is never re-classified.
type PropertyEntry struct {
	// KeyName is the original property field name (e.g., "plan_name", "error_code")
	KeyName string `json:"key_name"`

	// Role is the classified role: dimension, target, condition, or measure
	Role PropertyRole `json:"role"`

	// Label is a human-readable label (e.g., "plan" for key "plan_name")
	// Optional. AI can suggest, user can override.
	Label string `json:"label,omitempty"`

	// SampleValue stores the last seen value for reference/debugging
	SampleValue string `json:"sample_value,omitempty"`

	// Source indicates how this classification was determined:
	//   "builtin"  - from the built-in dimensions list
	//   "inferred" - type-inferred (numeric -> measure)
	//   "ai"       - classified by Property Agent AI
	//   "manual"   - user override from config
	Source string `json:"source"`

	// CreatedAt is the ISO timestamp of when this entry was created
	CreatedAt string `json:"created_at,omitempty"`
}
