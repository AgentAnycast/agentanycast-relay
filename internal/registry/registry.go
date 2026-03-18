// Package registry implements an in-memory Skill Registry with TTL-based expiration.
//
// Agents register their skills upon connecting to the Relay. The registry supports
// discovery by skill ID and automatic eviction when registrations expire or when
// agents disconnect.
package registry

import (
	"errors"
	"log/slog"
	"sync"
	"time"
)

// DefaultTTL is the default registration time-to-live.
const DefaultTTL = 30 * time.Second

// DefaultMaxRegistrations is the maximum number of concurrent registrations.
const DefaultMaxRegistrations = 4096

// DefaultDiscoverLimit is the default max results for a discovery query.
const DefaultDiscoverLimit = 100

// MaxDiscoverLimit is the hard upper bound for discovery results.
const MaxDiscoverLimit = 1000

// ErrRegistryFull is returned when the registry has reached its maximum capacity.
var ErrRegistryFull = errors.New("registry has reached maximum capacity")

// SkillInfo describes a single skill for registry purposes.
type SkillInfo struct {
	SkillID     string
	Description string
	Tags        map[string]string
}

// Registration represents a registered agent's skills.
type Registration struct {
	PeerID           string
	Skills           []SkillInfo
	AgentName        string
	AgentDescription string
	RegisteredAt     time.Time
	ExpiresAt        time.Time
}

// Config holds registry configuration.
type Config struct {
	TTL              time.Duration
	MaxRegistrations int
	CleanupInterval  time.Duration
	Logger           *slog.Logger
}

// Registry is a thread-safe, in-memory skill registry.
type Registry struct {
	mu      sync.RWMutex
	entries map[string]*Registration // keyed by peer_id
	config  Config
	logger  *slog.Logger
	stopCh  chan struct{}
}

// New creates a new Registry with the given configuration.
func New(cfg Config) *Registry {
	if cfg.TTL == 0 {
		cfg.TTL = DefaultTTL
	}
	if cfg.MaxRegistrations == 0 {
		cfg.MaxRegistrations = DefaultMaxRegistrations
	}
	if cfg.CleanupInterval == 0 {
		cfg.CleanupInterval = cfg.TTL / 2
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	r := &Registry{
		entries: make(map[string]*Registration),
		config:  cfg,
		logger:  cfg.Logger,
		stopCh:  make(chan struct{}),
	}

	go r.cleanupLoop()
	return r
}

// Register adds or refreshes an agent's skill registration.
// Returns the expiration time and an error if the registry is full.
func (r *Registry) Register(peerID string, skills []SkillInfo, agentName, agentDesc string) (time.Time, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Enforce capacity limit (allow updates to existing registrations).
	if _, exists := r.entries[peerID]; !exists {
		if len(r.entries) >= r.config.MaxRegistrations {
			return time.Time{}, ErrRegistryFull
		}
	}

	now := time.Now()
	expiresAt := now.Add(r.config.TTL)

	// Deep-copy tags to avoid referencing caller's maps.
	copiedSkills := make([]SkillInfo, len(skills))
	for i, s := range skills {
		var tags map[string]string
		if len(s.Tags) > 0 {
			tags = make(map[string]string, len(s.Tags))
			for k, v := range s.Tags {
				tags[k] = v
			}
		}
		copiedSkills[i] = SkillInfo{
			SkillID:     s.SkillID,
			Description: s.Description,
			Tags:        tags,
		}
	}

	r.entries[peerID] = &Registration{
		PeerID:           peerID,
		Skills:           copiedSkills,
		AgentName:        agentName,
		AgentDescription: agentDesc,
		RegisteredAt:     now,
		ExpiresAt:        expiresAt,
	}

	return expiresAt, nil
}

// Unregister removes specific skills for a peer. If skillIDs is empty,
// removes the entire registration.
func (r *Registry) Unregister(peerID string, skillIDs []string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(skillIDs) == 0 {
		delete(r.entries, peerID)
		r.logger.Info("peer unregistered", "peer_id", peerID)
		return
	}

	reg, ok := r.entries[peerID]
	if !ok {
		return
	}

	remove := make(map[string]struct{}, len(skillIDs))
	for _, id := range skillIDs {
		remove[id] = struct{}{}
	}

	filtered := make([]SkillInfo, 0, len(reg.Skills))
	for _, s := range reg.Skills {
		if _, found := remove[s.SkillID]; !found {
			filtered = append(filtered, s)
		}
	}

	if len(filtered) == 0 {
		delete(r.entries, peerID)
	} else {
		reg.Skills = filtered
	}
}

// DiscoverBySkill returns all non-expired registrations that offer the given skill.
// Optional tag filters are matched using AND semantics.
func (r *Registry) DiscoverBySkill(skillID string, tags map[string]string, limit int) []Registration {
	if limit <= 0 {
		limit = DefaultDiscoverLimit
	} else if limit > MaxDiscoverLimit {
		limit = MaxDiscoverLimit
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	now := time.Now()
	var results []Registration

	for _, reg := range r.entries {
		if now.After(reg.ExpiresAt) {
			continue
		}
		if hasSkill(reg, skillID, tags) {
			results = append(results, *reg)
			if len(results) >= limit {
				break
			}
		}
	}

	return results
}

// Heartbeat renews the TTL for an existing registration.
// Returns the new expiration time. Returns zero time if not found.
func (r *Registry) Heartbeat(peerID string) time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()

	reg, ok := r.entries[peerID]
	if !ok {
		return time.Time{}
	}

	reg.ExpiresAt = time.Now().Add(r.config.TTL)
	return reg.ExpiresAt
}

// RemovePeer removes all registrations for a peer (e.g., on disconnect).
func (r *Registry) RemovePeer(peerID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.entries[peerID]; ok {
		delete(r.entries, peerID)
		r.logger.Info("peer removed from registry (disconnect)", "peer_id", peerID)
	}
}

// Count returns the number of active (non-expired) registrations.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	now := time.Now()
	count := 0
	for _, reg := range r.entries {
		if now.Before(reg.ExpiresAt) {
			count++
		}
	}
	return count
}

// Close stops the cleanup goroutine.
func (r *Registry) Close() {
	close(r.stopCh)
}

func (r *Registry) cleanupLoop() {
	ticker := time.NewTicker(r.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			r.evictExpired()
		case <-r.stopCh:
			return
		}
	}
}

func (r *Registry) evictExpired() {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	evicted := 0
	for peerID, reg := range r.entries {
		if now.After(reg.ExpiresAt) {
			delete(r.entries, peerID)
			evicted++
		}
	}
	if evicted > 0 {
		r.logger.Debug("evicted expired registrations", "count", evicted, "remaining", len(r.entries))
	}
}

func hasSkill(reg *Registration, skillID string, tags map[string]string) bool {
	for _, s := range reg.Skills {
		if s.SkillID != skillID {
			continue
		}
		if matchTags(s.Tags, tags) {
			return true
		}
	}
	return false
}

func matchTags(skillTags, filterTags map[string]string) bool {
	for k, v := range filterTags {
		if skillTags[k] != v {
			return false
		}
	}
	return true
}
