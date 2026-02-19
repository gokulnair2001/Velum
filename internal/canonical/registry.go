package canonical

import "fmt"

// CoreFields are the fields already handled by the pipeline.
// These are never classified as context properties.
var CoreFields = map[string]bool{
	"id":         true,
	"ts":         true,
	"timestamp":  true,
	"event":      true,
	"event_name": true,
	"eventName":  true,
	"name":       true,
	"action":     true,
	"type":       true,
	"user_id":    true,
	"userId":     true,
	"session_id": true,
	"sessionId":  true,
	ContextKey:   true,
}

// BuiltinDimensions maps known analytics dimension field names to their
// normalized label. Multiple field names can map to the same normalized
// label (e.g., "device", "device_type", "deviceType" all map to "device").
// These are resolved without AI.
var BuiltinDimensions = map[string]string{
	// Device
	"device":       "device",
	"device_type":  "device",
	"deviceType":   "device",
	"device_model": "device_model",
	"deviceModel":  "device_model",

	// Platform / OS
	"platform":   "platform",
	"os":         "platform",
	"os_name":    "platform",
	"osName":     "platform",
	"os_version": "os_version",
	"osVersion":  "os_version",

	// Browser
	"browser":         "browser",
	"browser_name":    "browser",
	"browserName":     "browser",
	"browser_version": "browser_version",
	"browserVersion":  "browser_version",

	// Geography
	"country":  "country",
	"region":   "region",
	"city":     "city",
	"locale":   "locale",
	"timezone": "timezone",
	"tz":       "timezone",

	// App versioning
	"app_version":   "app_version",
	"appVersion":    "app_version",
	"build":         "build",
	"build_version": "build",
	"buildVersion":  "build",
	"version":       "app_version",

	// Attribution / Channel
	"channel":      "channel",
	"source":       "source",
	"medium":       "medium",
	"utm_source":   "utm_source",
	"utmSource":    "utm_source",
	"utm_medium":   "utm_medium",
	"utmMedium":    "utm_medium",
	"utm_campaign": "utm_campaign",
	"utmCampaign":  "utm_campaign",
	"utm_term":     "utm_term",
	"utm_content":  "utm_content",
	"referrer":     "referrer",
	"ref":          "referrer",

	// Environment
	"environment": "environment",
	"env":         "environment",

	// Network
	"network_type":    "network_type",
	"networkType":     "network_type",
	"connection_type": "network_type",
	"connectionType":  "network_type",

	// Screen
	"screen_resolution": "screen_resolution",
	"viewport":          "viewport",

	// Language
	"language": "language",
	"lang":     "language",

	// User segmentation
	"user_type":    "user_type",
	"userType":     "user_type",
	"user_role":    "user_role",
	"userRole":     "user_role",
	"user_segment": "user_segment",
	"userSegment":  "user_segment",
}

// IsCoreField returns true if the field is a core pipeline field
// and should not be classified as a context property.
func IsCoreField(key string) bool {
	return CoreFields[key]
}

// RecommendedProperty describes a property that Velum recommends including in events
// for optimal behavioral analysis.
type RecommendedProperty struct {
	// Names lists all accepted field name variants for this property.
	Names []string
	// Label is the human-readable name shown in warnings.
	Label string
	// Description explains why this property matters.
	Description string
}

// RecommendedProperties lists properties that Velum recommends including in events.
// These are not required, but significantly improve analysis quality.
var RecommendedProperties = []RecommendedProperty{
	{
		Names:       []string{"device", "device_type", "deviceType"},
		Label:       "device",
		Description: "Mobile vs desktop behavior differs significantly",
	},
	{
		Names:       []string{"country", "region"},
		Label:       "country/region",
		Description: "Regional patterns and latency differences",
	},
	{
		Names:       []string{"platform", "os", "os_name", "osName"},
		Label:       "platform",
		Description: "OS-specific behavioral patterns",
	},
}

// CheckRecommendedProperties checks a batch of events for missing recommended properties.
// Returns a list of warning strings for properties not found in any event.
func CheckRecommendedProperties(events []map[string]interface{}) []string {
	if len(events) == 0 {
		return nil
	}

	// Track which recommended properties appear in at least one event
	found := make(map[string]bool, len(RecommendedProperties))
	for _, rp := range RecommendedProperties {
		found[rp.Label] = false
	}

	for _, event := range events {
		for _, rp := range RecommendedProperties {
			if found[rp.Label] {
				continue // already found
			}
			for _, name := range rp.Names {
				if _, ok := event[name]; ok {
					found[rp.Label] = true
					break
				}
			}
		}
	}

	var warnings []string
	for _, rp := range RecommendedProperties {
		if !found[rp.Label] {
			warnings = append(warnings, fmt.Sprintf(
				"recommended property '%s' not found in events (%s). Accepted field names: %v",
				rp.Label, rp.Description, rp.Names,
			))
		}
	}
	return warnings
}

// IsDimension checks if a field name is a known analytics dimension.
// Returns the normalized label and true if found.
func IsDimension(key string) (string, bool) {
	label, ok := BuiltinDimensions[key]
	return label, ok
}

// IsMeasureValue checks if a value is numeric, indicating it is likely a measure.
// Returns the float64 representation and true if numeric.
func IsMeasureValue(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case int32:
		return float64(v), true
	case int16:
		return float64(v), true
	case int8:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint64:
		return float64(v), true
	case uint32:
		return float64(v), true
	default:
		return 0, false
	}
}

// FormatSampleValue converts a value to a string representation for storage.
func FormatSampleValue(value interface{}) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%v", value)
}
