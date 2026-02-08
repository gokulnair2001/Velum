package behavior

// BehaviorType represents the type of user behavior
type BehaviorType string

const (
	BehaviorExplore  BehaviorType = "explore"
	BehaviorAttempt  BehaviorType = "attempt"
	BehaviorRetry    BehaviorType = "retry"
	BehaviorSucceed  BehaviorType = "succeed"
	BehaviorAbandon  BehaviorType = "abandon"
	BehaviorHesitate BehaviorType = "hesitate"
	BehaviorBypass   BehaviorType = "bypass"
)

// BehaviorPriority defines the priority order (higher = more important)
var BehaviorPriority = map[BehaviorType]int{
	BehaviorSucceed:  5, // highest
	BehaviorRetry:    4,
	BehaviorAttempt:  3,
	BehaviorAbandon:  2,
	BehaviorHesitate: 1,
	BehaviorExplore:  0, // lowest
	BehaviorBypass:   0,
}

// BehaviorEvent represents a behavior occurrence within a flow
type BehaviorEvent struct {
	Behavior  BehaviorType `json:"behavior"`
	EventIndex int         `json:"event_index"` // Index of the event that triggered this behavior
	Reason    string       `json:"reason"`      // Explanation for the behavior
}

// Config holds configuration for behavior detection
type Config struct {
	// ActionStatuses are statuses that indicate user action (not just viewing)
	ActionStatuses map[string]bool

	// EntryStatuses are statuses that indicate flow entry
	EntryStatuses map[string]bool

	// ErrorStatuses are statuses that indicate errors
	ErrorStatuses map[string]bool

	// SuccessStatuses are statuses that indicate success
	SuccessStatuses map[string]bool

	// ExitStatuses are statuses that indicate explicit exit
	ExitStatuses map[string]bool

	// EnableHesitation controls whether to detect hesitation (conservative)
	EnableHesitation bool
}

// DefaultConfig returns the default behavior detection configuration
func DefaultConfig() *Config {
	return &Config{
		ActionStatuses: map[string]bool{
			"click":   true,
			"submit":  true,
			"confirm": true,
			"select":  true,
		},
		EntryStatuses: map[string]bool{
			"view":  true,
			"start": true,
		},
		ErrorStatuses: map[string]bool{
			"error":  true,
			"failed": true,
		},
		SuccessStatuses: map[string]bool{
			"success": true,
		},
		ExitStatuses: map[string]bool{
			"exit":    true,
			"dismiss": true,
			"end":     true,
		},
		EnableHesitation: true,
	}
}
