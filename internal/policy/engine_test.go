package policy

import (
	"log/slog"
	"os"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestEngine_DefaultAllow(t *testing.T) {
	e := NewEngine(testLogger())
	d := e.Evaluate("did:key:z123", "send", "skill-echo", "default")
	if d != DecisionAllow {
		t.Errorf("expected allow with no rules, got %s", d)
	}
}

func TestEngine_DenyRule(t *testing.T) {
	e := NewEngine(testLogger())
	err := e.AddRule(Rule{
		ID:       "deny-all",
		Priority: 1,
		Subject:  "*",
		Action:   "*",
		Resource: "*",
		Effect:   "deny",
	})
	if err != nil {
		t.Fatal(err)
	}

	d := e.Evaluate("did:key:z123", "send", "skill-echo", "default")
	if d != DecisionDeny {
		t.Errorf("expected deny, got %s", d)
	}
}

func TestEngine_AllowRule(t *testing.T) {
	e := NewEngine(testLogger())
	_ = e.AddRule(Rule{
		ID:       "allow-echo",
		Priority: 1,
		Subject:  "*",
		Action:   "send",
		Resource: "skill-echo",
		Effect:   "allow",
	})

	d := e.Evaluate("did:key:z123", "send", "skill-echo", "")
	if d != DecisionAllow {
		t.Errorf("expected allow, got %s", d)
	}
}

func TestEngine_RateLimitRule(t *testing.T) {
	e := NewEngine(testLogger())
	_ = e.AddRule(Rule{
		ID:       "rate-limit-discover",
		Priority: 1,
		Subject:  "*",
		Action:   "discover",
		Resource: "*",
		Effect:   "rate_limit",
		RateLimit: &RateLimitSpec{
			RequestsPerSecond: 10,
			BurstSize:         20,
		},
	})

	d := e.Evaluate("did:key:z123", "discover", "skill-echo", "")
	if d != DecisionRateLimit {
		t.Errorf("expected rate_limit, got %s", d)
	}
}

func TestEngine_PriorityOrdering(t *testing.T) {
	e := NewEngine(testLogger())

	// Lower priority number = higher priority. The allow rule (priority 1)
	// should win over the deny rule (priority 10).
	_ = e.AddRule(Rule{
		ID:       "deny-all",
		Priority: 10,
		Subject:  "*",
		Action:   "*",
		Resource: "*",
		Effect:   "deny",
	})
	_ = e.AddRule(Rule{
		ID:       "allow-echo",
		Priority: 1,
		Subject:  "*",
		Action:   "send",
		Resource: "skill-echo",
		Effect:   "allow",
	})

	d := e.Evaluate("did:key:z123", "send", "skill-echo", "")
	if d != DecisionAllow {
		t.Errorf("expected allow (higher priority), got %s", d)
	}
}

func TestEngine_GlobMatching(t *testing.T) {
	e := NewEngine(testLogger())
	_ = e.AddRule(Rule{
		ID:       "deny-skill-prefix",
		Priority: 1,
		Subject:  "did:key:z*",
		Action:   "*",
		Resource: "skill-*",
		Effect:   "deny",
	})

	// Should match glob pattern.
	d := e.Evaluate("did:key:z123abc", "send", "skill-echo", "")
	if d != DecisionDeny {
		t.Errorf("expected deny for glob match, got %s", d)
	}

	// Should not match different subject prefix.
	d = e.Evaluate("did:key:a999", "send", "skill-echo", "")
	if d != DecisionAllow {
		t.Errorf("expected allow for non-matching subject, got %s", d)
	}
}

func TestEngine_NamespaceFiltering(t *testing.T) {
	e := NewEngine(testLogger())
	_ = e.AddRule(Rule{
		ID:        "deny-prod",
		Priority:  1,
		Subject:   "*",
		Action:    "*",
		Resource:  "*",
		Effect:    "deny",
		Namespace: "production",
	})

	// Should deny in production namespace.
	d := e.Evaluate("did:key:z123", "send", "skill-echo", "production")
	if d != DecisionDeny {
		t.Errorf("expected deny in production namespace, got %s", d)
	}

	// Should allow in different namespace (rule doesn't apply).
	d = e.Evaluate("did:key:z123", "send", "skill-echo", "staging")
	if d != DecisionAllow {
		t.Errorf("expected allow in staging namespace, got %s", d)
	}
}

func TestEngine_AddDuplicateRule(t *testing.T) {
	e := NewEngine(testLogger())
	_ = e.AddRule(Rule{ID: "r1", Priority: 1, Subject: "*", Action: "*", Resource: "*", Effect: "allow"})
	err := e.AddRule(Rule{ID: "r1", Priority: 2, Subject: "*", Action: "*", Resource: "*", Effect: "deny"})
	if err == nil {
		t.Error("expected error for duplicate rule ID")
	}
}

func TestEngine_RemoveRule(t *testing.T) {
	e := NewEngine(testLogger())
	_ = e.AddRule(Rule{ID: "r1", Priority: 1, Subject: "*", Action: "*", Resource: "*", Effect: "deny"})

	err := e.RemoveRule("r1")
	if err != nil {
		t.Fatal(err)
	}

	// After removal, default allow should apply.
	d := e.Evaluate("did:key:z123", "send", "skill-echo", "")
	if d != DecisionAllow {
		t.Errorf("expected allow after rule removal, got %s", d)
	}

	// Removing non-existent rule should error.
	err = e.RemoveRule("nonexistent")
	if err == nil {
		t.Error("expected error removing non-existent rule")
	}
}

func TestEngine_UpdateRule(t *testing.T) {
	e := NewEngine(testLogger())
	_ = e.AddRule(Rule{ID: "r1", Priority: 1, Subject: "*", Action: "*", Resource: "*", Effect: "deny"})

	// Update to allow.
	err := e.UpdateRule(Rule{ID: "r1", Priority: 1, Subject: "*", Action: "*", Resource: "*", Effect: "allow"})
	if err != nil {
		t.Fatal(err)
	}

	d := e.Evaluate("did:key:z123", "send", "skill-echo", "")
	if d != DecisionAllow {
		t.Errorf("expected allow after update, got %s", d)
	}

	// Update non-existent rule should error.
	err = e.UpdateRule(Rule{ID: "nonexistent"})
	if err == nil {
		t.Error("expected error updating non-existent rule")
	}
}

func TestEngine_ListRules(t *testing.T) {
	e := NewEngine(testLogger())
	_ = e.AddRule(Rule{ID: "r1", Priority: 1, Subject: "*", Action: "*", Resource: "*", Effect: "allow"})
	_ = e.AddRule(Rule{ID: "r2", Priority: 2, Subject: "*", Action: "*", Resource: "*", Effect: "deny"})

	rules := e.ListRules()
	if len(rules) != 2 {
		t.Errorf("expected 2 rules, got %d", len(rules))
	}
}
