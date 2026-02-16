package datamapper

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/velum/internal/config"
)

// DataMapper is a layer that transforms raw events to a standardized format
// using declarative field mappings from configuration
type DataMapper struct {
	config config.DataMappingConfig
}

// New creates a new DataMapper with the given configuration
func New(cfg config.DataMappingConfig) *DataMapper {
	return &DataMapper{
		config: cfg,
	}
}

// Name returns the layer identifier
func (d *DataMapper) Name() string {
	return "data_mapper"
}

// Process implements the Layer interface
// Takes raw events and maps them to the standardized format
func (d *DataMapper) Process(input interface{}) (interface{}, error) {
	if !d.config.Enabled {
		return input, nil
	}

	switch v := input.(type) {
	case map[string]interface{}:
		return d.mapSingleEvent(v, 0)
	case []map[string]interface{}:
		results := make([]map[string]interface{}, 0, len(v))
		for i, event := range v {
			mapped, err := d.mapSingleEvent(event, i)
			if err != nil {
				return nil, err
			}
			results = append(results, mapped)
		}
		return results, nil
	case []interface{}:
		// Handle generic interface slice (common from JSON unmarshaling)
		results := make([]map[string]interface{}, 0, len(v))
		for i, item := range v {
			if event, ok := item.(map[string]interface{}); ok {
				mapped, err := d.mapSingleEvent(event, i)
				if err != nil {
					return nil, err
				}
				results = append(results, mapped)
			} else {
				return nil, fmt.Errorf("event at index %d is not a valid object", i)
			}
		}
		return results, nil
	default:
		// Pass through unknown types
		return input, nil
	}
}

// mapSingleEvent transforms a single raw event using the configured mapping
func (d *DataMapper) mapSingleEvent(raw map[string]interface{}, index int) (map[string]interface{}, error) {
	result := make(map[string]interface{})

	for fieldName, spec := range d.config.Mapping {
		value, found := d.extractValue(raw, spec.Paths)

		if !found {
			if spec.Required {
				return nil, fmt.Errorf("event at index %d missing mandatory field: %s (tried paths: %v)", 
					index, fieldName, spec.Paths)
			}
			// Optional field missing - omit entirely
			continue
		}

		// Apply format transformation if specified
		if spec.Format != "" {
			transformed, err := d.applyFormat(value, spec.Format, fieldName, index)
			if err != nil {
				return nil, err
			}
			value = transformed
		}

		result[fieldName] = value
	}

	return result, nil
}

// extractValue tries each path in order and returns the first found value
func (d *DataMapper) extractValue(data map[string]interface{}, paths []string) (interface{}, bool) {
	for _, path := range paths {
		if value, found := d.getNestedValue(data, path); found {
			return value, true
		}
	}
	return nil, false
}

// getNestedValue retrieves a value from a nested map using dot notation
// e.g., "payload.event.action" from {"payload": {"event": {"action": "click"}}}
func (d *DataMapper) getNestedValue(data map[string]interface{}, path string) (interface{}, bool) {
	parts := strings.Split(path, ".")
	current := interface{}(data)

	for _, part := range parts {
		switch v := current.(type) {
		case map[string]interface{}:
			val, ok := v[part]
			if !ok {
				return nil, false
			}
			current = val
		default:
			return nil, false
		}
	}

	return current, true
}

// applyFormat transforms a value according to the specified format
func (d *DataMapper) applyFormat(value interface{}, format, fieldName string, index int) (interface{}, error) {
	switch format {
	case "epoch_ms":
		return d.parseTimestamp(value, 1, fieldName, index)
	case "epoch_s":
		return d.parseTimestamp(value, 1000, fieldName, index)
	case "iso8601":
		return d.parseISO8601(value, fieldName, index)
	default:
		// Unknown format, pass through
		return value, nil
	}
}

// parseTimestamp converts various timestamp formats to epoch milliseconds
func (d *DataMapper) parseTimestamp(value interface{}, multiplier int64, fieldName string, index int) (int64, error) {
	switch v := value.(type) {
	case float64:
		return int64(v) * multiplier, nil
	case int64:
		return v * multiplier, nil
	case int:
		return int64(v) * multiplier, nil
	case string:
		// Try parsing as number
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n * multiplier, nil
		}
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return int64(f) * multiplier, nil
		}
		return 0, fmt.Errorf("event at index %d: cannot parse '%s' as timestamp for field %s", 
			index, v, fieldName)
	default:
		return 0, fmt.Errorf("event at index %d: unsupported type %T for timestamp field %s", 
			index, value, fieldName)
	}
}

// parseISO8601 parses an ISO8601 string and converts to epoch milliseconds
func (d *DataMapper) parseISO8601(value interface{}, fieldName string, index int) (int64, error) {
	str, ok := value.(string)
	if !ok {
		return 0, fmt.Errorf("event at index %d: expected string for ISO8601 field %s, got %T", 
			index, fieldName, value)
	}

	// Try common ISO8601 layouts
	layouts := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}

	for _, layout := range layouts {
		if t, err := time.Parse(layout, str); err == nil {
			return t.UnixMilli(), nil
		}
	}

	return 0, fmt.Errorf("event at index %d: cannot parse '%s' as ISO8601 for field %s", 
		index, str, fieldName)
}

// IsEnabled returns whether the data mapper is enabled
func (d *DataMapper) IsEnabled() bool {
	return d.config.Enabled
}
