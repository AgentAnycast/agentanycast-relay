package registry

import (
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestRegisterAndDiscover(t *testing.T) {
	reg := New(Config{TTL: 5 * time.Second, Logger: testLogger()})
	defer reg.Close()

	skills := []SkillInfo{
		{SkillID: "summarize", Description: "Summarize text"},
		{SkillID: "translate", Description: "Translate text"},
	}

	_, err := reg.Register("peer-1", skills, "Agent A", "Test agent")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	results := reg.DiscoverBySkill("summarize", nil, 0, false)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].PeerID != "peer-1" {
		t.Fatalf("expected peer-1, got %s", results[0].PeerID)
	}
	if results[0].AgentName != "Agent A" {
		t.Fatalf("expected Agent A, got %s", results[0].AgentName)
	}

	// Discover a non-existent skill.
	results = reg.DiscoverBySkill("nonexistent", nil, 0, false)
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestTagFiltering(t *testing.T) {
	reg := New(Config{TTL: 5 * time.Second, Logger: testLogger()})
	defer reg.Close()

	_, _ = reg.Register("peer-1", []SkillInfo{
		{SkillID: "translate", Tags: map[string]string{"lang": "en"}},
	}, "Agent EN", "")
	_, _ = reg.Register("peer-2", []SkillInfo{
		{SkillID: "translate", Tags: map[string]string{"lang": "zh"}},
	}, "Agent ZH", "")

	// Filter for lang=zh.
	results := reg.DiscoverBySkill("translate", map[string]string{"lang": "zh"}, 0, false)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].PeerID != "peer-2" {
		t.Fatalf("expected peer-2, got %s", results[0].PeerID)
	}

	// No filter returns both.
	results = reg.DiscoverBySkill("translate", nil, 0, false)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}

func TestMaxRegistrations(t *testing.T) {
	reg := New(Config{TTL: 5 * time.Second, MaxRegistrations: 2, Logger: testLogger()})
	defer reg.Close()

	skills := []SkillInfo{{SkillID: "echo"}}

	_, err := reg.Register("peer-1", skills, "A", "")
	if err != nil {
		t.Fatalf("Register peer-1: %v", err)
	}
	_, err = reg.Register("peer-2", skills, "B", "")
	if err != nil {
		t.Fatalf("Register peer-2: %v", err)
	}

	// Third should fail.
	_, err = reg.Register("peer-3", skills, "C", "")
	if err != ErrRegistryFull {
		t.Fatalf("expected ErrRegistryFull, got %v", err)
	}

	// Updating existing peer should succeed.
	_, err = reg.Register("peer-1", skills, "A-updated", "")
	if err != nil {
		t.Fatalf("Update peer-1: %v", err)
	}
}

func TestTTLExpiration(t *testing.T) {
	reg := New(Config{TTL: 50 * time.Millisecond, CleanupInterval: 25 * time.Millisecond, Logger: testLogger()})
	defer reg.Close()

	_, _ = reg.Register("peer-1", []SkillInfo{{SkillID: "echo"}}, "A", "")

	// Immediately discoverable.
	if len(reg.DiscoverBySkill("echo", nil, 0, false)) != 1 {
		t.Fatal("expected 1 result before expiry")
	}

	// Wait for expiry + cleanup.
	time.Sleep(100 * time.Millisecond)

	if len(reg.DiscoverBySkill("echo", nil, 0, false)) != 0 {
		t.Fatal("expected 0 results after expiry")
	}
}

func TestHeartbeatRenewal(t *testing.T) {
	reg := New(Config{TTL: 50 * time.Millisecond, CleanupInterval: 25 * time.Millisecond, Logger: testLogger()})
	defer reg.Close()

	_, _ = reg.Register("peer-1", []SkillInfo{{SkillID: "echo"}}, "A", "")

	// Heartbeat before expiry.
	time.Sleep(30 * time.Millisecond)
	newExpiry := reg.Heartbeat("peer-1")
	if newExpiry.IsZero() {
		t.Fatal("heartbeat returned zero time")
	}

	// Should still be discoverable after original TTL would have expired.
	time.Sleep(30 * time.Millisecond)
	if len(reg.DiscoverBySkill("echo", nil, 0, false)) != 1 {
		t.Fatal("expected 1 result after heartbeat renewal")
	}

	// Heartbeat for unknown peer returns zero.
	if !reg.Heartbeat("unknown").IsZero() {
		t.Fatal("expected zero time for unknown peer")
	}
}

func TestRemovePeer(t *testing.T) {
	reg := New(Config{TTL: 5 * time.Second, Logger: testLogger()})
	defer reg.Close()

	_, _ = reg.Register("peer-1", []SkillInfo{{SkillID: "echo"}}, "A", "")
	reg.RemovePeer("peer-1")

	if len(reg.DiscoverBySkill("echo", nil, 0, false)) != 0 {
		t.Fatal("expected 0 results after RemovePeer")
	}
}

func TestUnregisterSpecificSkills(t *testing.T) {
	reg := New(Config{TTL: 5 * time.Second, Logger: testLogger()})
	defer reg.Close()

	_, _ = reg.Register("peer-1", []SkillInfo{
		{SkillID: "summarize"},
		{SkillID: "translate"},
	}, "A", "")

	reg.Unregister("peer-1", []string{"summarize"})

	if len(reg.DiscoverBySkill("summarize", nil, 0, false)) != 0 {
		t.Fatal("expected 0 results for removed skill")
	}
	if len(reg.DiscoverBySkill("translate", nil, 0, false)) != 1 {
		t.Fatal("expected 1 result for remaining skill")
	}
}

func TestDiscoverLimit(t *testing.T) {
	reg := New(Config{TTL: 5 * time.Second, Logger: testLogger()})
	defer reg.Close()

	for i := 0; i < 10; i++ {
		_, _ = reg.Register(
			"peer-"+string(rune('a'+i)),
			[]SkillInfo{{SkillID: "echo"}},
			"Agent", "",
		)
	}

	// Explicit limit.
	results := reg.DiscoverBySkill("echo", nil, 3, false)
	if len(results) != 3 {
		t.Fatalf("expected 3 results with limit=3, got %d", len(results))
	}

	// Default limit (0 → DefaultDiscoverLimit=100).
	results = reg.DiscoverBySkill("echo", nil, 0, false)
	if len(results) != 10 {
		t.Fatalf("expected 10 results with default limit, got %d", len(results))
	}
}

func TestConcurrentAccess(t *testing.T) {
	reg := New(Config{TTL: 5 * time.Second, Logger: testLogger()})
	defer reg.Close()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(3)
		peerID := "peer-" + string(rune('a'+i%26))
		go func() {
			defer wg.Done()
			_, _ = reg.Register(peerID, []SkillInfo{{SkillID: "echo"}}, "A", "")
		}()
		go func() {
			defer wg.Done()
			_ = reg.DiscoverBySkill("echo", nil, 0, false)
		}()
		go func() {
			defer wg.Done()
			_ = reg.Heartbeat(peerID)
		}()
	}
	wg.Wait()
}

func TestStats(t *testing.T) {
	reg := New(Config{TTL: 5 * time.Second, Logger: testLogger()})
	defer reg.Close()

	// Empty registry.
	stats := reg.Stats()
	if stats.LocalCount != 0 || stats.FederatedCount != 0 || stats.SkillCount != 0 {
		t.Fatalf("expected empty stats, got %+v", stats)
	}

	// Register some agents.
	_, _ = reg.Register("peer-1", []SkillInfo{
		{SkillID: "summarize"},
		{SkillID: "translate"},
	}, "Agent A", "")
	_, _ = reg.Register("peer-2", []SkillInfo{
		{SkillID: "summarize"},
	}, "Agent B", "")

	stats = reg.Stats()
	if stats.LocalCount != 2 {
		t.Fatalf("expected 2 local, got %d", stats.LocalCount)
	}
	if stats.SkillCount != 2 {
		t.Fatalf("expected 2 skills, got %d", stats.SkillCount)
	}

	// Add a federated registration.
	now := time.Now()
	_ = reg.RegisterFederated("peer-3", "relay-2", []SkillInfo{
		{SkillID: "code"},
	}, "Agent C", "", now, now.Add(5*time.Second), uint64(now.UnixNano()))

	stats = reg.Stats()
	if stats.FederatedCount != 1 {
		t.Fatalf("expected 1 federated, got %d", stats.FederatedCount)
	}
	if stats.FedSkillCount != 1 {
		t.Fatalf("expected 1 fed skill, got %d", stats.FedSkillCount)
	}
}
