// Package policy provides a dynamic policy engine for access control and rate limiting.
package policy

// Rule represents an access control or rate limit policy rule.
type Rule struct {
	ID        string         `json:"id"`
	Priority  int            `json:"priority"`              // Lower = higher priority.
	Subject   string         `json:"subject"`               // DID pattern or "*".
	Action    string         `json:"action"`                // "send", "receive", "discover", "*".
	Resource  string         `json:"resource"`              // Skill pattern or "*".
	Effect    string         `json:"effect"`                // "allow", "deny", "rate_limit".
	RateLimit *RateLimitSpec `json:"rate_limit,omitempty"`  // Only when effect="rate_limit".
	Namespace string         `json:"namespace,omitempty"`
}

// RateLimitSpec defines rate limiting parameters for a policy rule.
type RateLimitSpec struct {
	RequestsPerSecond int `json:"requests_per_second"`
	BurstSize         int `json:"burst_size"`
}

// Decision represents the result of a policy evaluation.
type Decision string

const (
	// DecisionAllow permits the request.
	DecisionAllow Decision = "allow"
	// DecisionDeny rejects the request.
	DecisionDeny Decision = "deny"
	// DecisionRateLimit applies rate limiting to the request.
	DecisionRateLimit Decision = "rate_limit"
)
