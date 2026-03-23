package policy

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"sync"
)

// Engine evaluates policy rules against requests.
type Engine struct {
	mu     sync.RWMutex
	rules  []Rule
	logger *slog.Logger
}

// NewEngine creates a new policy engine.
func NewEngine(logger *slog.Logger) *Engine {
	return &Engine{
		rules:  make([]Rule, 0),
		logger: logger,
	}
}

// Evaluate checks a request against all policy rules and returns the decision.
// Rules are evaluated in priority order (lower number = higher priority).
// If no rule matches, the default decision is "allow" (open by default).
func (e *Engine) Evaluate(subject, action, resource, namespace string) Decision {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Sort a copy by priority for evaluation.
	sorted := make([]Rule, len(e.rules))
	copy(sorted, e.rules)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority < sorted[j].Priority
	})

	for _, rule := range sorted {
		if !matchGlob(rule.Subject, subject) {
			continue
		}
		if !matchGlob(rule.Action, action) {
			continue
		}
		if !matchGlob(rule.Resource, resource) {
			continue
		}
		if rule.Namespace != "" && rule.Namespace != namespace {
			continue
		}

		e.logger.Debug("policy rule matched",
			"rule_id", rule.ID,
			"effect", rule.Effect,
			"subject", subject,
			"action", action,
			"resource", resource,
		)
		return Decision(rule.Effect)
	}

	return DecisionAllow
}

// AddRule adds a new policy rule. Returns an error if a rule with the same ID already exists.
func (e *Engine) AddRule(rule Rule) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	for _, r := range e.rules {
		if r.ID == rule.ID {
			return fmt.Errorf("rule with id %q already exists", rule.ID)
		}
	}

	e.rules = append(e.rules, rule)
	e.logger.Info("policy rule added", "rule_id", rule.ID, "effect", rule.Effect)
	return nil
}

// RemoveRule removes a policy rule by ID. Returns an error if the rule is not found.
func (e *Engine) RemoveRule(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	for i, r := range e.rules {
		if r.ID == id {
			e.rules = append(e.rules[:i], e.rules[i+1:]...)
			e.logger.Info("policy rule removed", "rule_id", id)
			return nil
		}
	}
	return fmt.Errorf("rule with id %q not found", id)
}

// ListRules returns a copy of all policy rules.
func (e *Engine) ListRules() []Rule {
	e.mu.RLock()
	defer e.mu.RUnlock()

	result := make([]Rule, len(e.rules))
	copy(result, e.rules)
	return result
}

// UpdateRule replaces an existing rule. Returns an error if the rule is not found.
func (e *Engine) UpdateRule(rule Rule) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	for i, r := range e.rules {
		if r.ID == rule.ID {
			e.rules[i] = rule
			e.logger.Info("policy rule updated", "rule_id", rule.ID)
			return nil
		}
	}
	return fmt.Errorf("rule with id %q not found", rule.ID)
}

// matchGlob performs glob-style pattern matching. The pattern supports "*" as
// a wildcard. A standalone "*" matches anything. Uses filepath.Match semantics.
func matchGlob(pattern, value string) bool {
	if pattern == "*" {
		return true
	}
	matched, err := filepath.Match(pattern, value)
	if err != nil {
		return false
	}
	return matched
}
