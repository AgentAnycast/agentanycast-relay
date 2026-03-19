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

// DefaultMaxRegistrations is the maximum number of concurrent local registrations.
const DefaultMaxRegistrations = 4096

// DefaultMaxFederatedRegistrations is the maximum number of concurrent federated registrations.
const DefaultMaxFederatedRegistrations = 8192

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
	Origin           string // empty = local registration, relay_id = federated from another relay
	Version          uint64 // Lamport clock for last-writer-wins conflict resolution
}

// Config holds registry configuration.
type Config struct {
	TTL                      time.Duration
	MaxRegistrations         int
	MaxFederatedRegistrations int
	CleanupInterval          time.Duration
	Logger                   *slog.Logger
}

// Registry is a thread-safe, in-memory skill registry.
type Registry struct {
	mu        sync.RWMutex
	entries   map[string]*Registration // keyed by peer_id (local registrations)
	federated map[string]*Registration // keyed by peer_id (federated registrations)
	config    Config
	logger    *slog.Logger
	stopCh    chan struct{}
	closeOnce sync.Once
}

// New creates a new Registry with the given configuration.
func New(cfg Config) *Registry {
	if cfg.TTL == 0 {
		cfg.TTL = DefaultTTL
	}
	if cfg.MaxRegistrations == 0 {
		cfg.MaxRegistrations = DefaultMaxRegistrations
	}
	if cfg.MaxFederatedRegistrations == 0 {
		cfg.MaxFederatedRegistrations = DefaultMaxFederatedRegistrations
	}
	if cfg.CleanupInterval == 0 {
		cfg.CleanupInterval = cfg.TTL / 2
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	r := &Registry{
		entries:   make(map[string]*Registration),
		federated: make(map[string]*Registration),
		config:    cfg,
		logger:    cfg.Logger,
		stopCh:    make(chan struct{}),
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
		Version:          uint64(now.UnixNano()),
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
// When includeFederated is true, results also include federated entries from peer relays.
func (r *Registry) DiscoverBySkill(skillID string, tags map[string]string, limit int, includeFederated bool) []Registration {
	if limit <= 0 {
		limit = DefaultDiscoverLimit
	} else if limit > MaxDiscoverLimit {
		limit = MaxDiscoverLimit
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	now := time.Now()
	var results []Registration

	// Always search local entries first.
	localPeers := make(map[string]struct{})
	for _, reg := range r.entries {
		if now.After(reg.ExpiresAt) {
			continue
		}
		if hasSkill(reg, skillID, tags) {
			results = append(results, *reg)
			localPeers[reg.PeerID] = struct{}{}
			if len(results) >= limit {
				return results
			}
		}
	}

	// Optionally include federated entries, skipping peers already found locally.
	if includeFederated {
		for _, reg := range r.federated {
			if now.After(reg.ExpiresAt) {
				continue
			}
			if _, isLocal := localPeers[reg.PeerID]; isLocal {
				continue // local registration takes priority
			}
			if hasSkill(reg, skillID, tags) {
				results = append(results, *reg)
				if len(results) >= limit {
					break
				}
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

// AllRegistrations returns all non-expired registrations.
func (r *Registry) AllRegistrations() []Registration {
	r.mu.RLock()
	defer r.mu.RUnlock()

	now := time.Now()
	var results []Registration
	for _, reg := range r.entries {
		if now.Before(reg.ExpiresAt) {
			results = append(results, *reg)
		}
	}
	return results
}

// Close stops the cleanup goroutine. It is safe to call multiple times.
func (r *Registry) Close() {
	r.closeOnce.Do(func() {
		close(r.stopCh)
	})
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
	fedEvicted := 0
	for peerID, reg := range r.federated {
		if now.After(reg.ExpiresAt) {
			delete(r.federated, peerID)
			fedEvicted++
		}
	}
	if evicted > 0 || fedEvicted > 0 {
		r.logger.Debug("evicted expired registrations",
			"local", evicted,
			"federated", fedEvicted,
			"remaining_local", len(r.entries),
			"remaining_federated", len(r.federated),
		)
	}
}

// ErrFederatedRegistryFull is returned when the federated registry has reached its maximum capacity.
var ErrFederatedRegistryFull = errors.New("federated registry has reached maximum capacity")

// RegisterFederated stores a federated registration from another relay.
// Uses LWW (last-writer-wins): only accepts if version >= existing version.
func (r *Registry) RegisterFederated(peerID, originRelayID string, skills []SkillInfo, agentName, agentDesc string, registeredAt, expiresAt time.Time, version uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check LWW: reject if existing version is newer.
	// On equal versions, use lexicographic comparison of origin relay ID as tiebreaker
	// (lower relay ID wins).
	if existing, ok := r.federated[peerID]; ok {
		if existing.Version > version {
			return nil // silently ignore stale update
		}
		if existing.Version == version && existing.Origin <= originRelayID {
			return nil // tiebreaker: existing origin wins (lower or equal relay ID)
		}
	} else {
		// New entry — enforce capacity limit.
		if len(r.federated) >= r.config.MaxFederatedRegistrations {
			return ErrFederatedRegistryFull
		}
	}

	// Deep-copy tags.
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

	r.federated[peerID] = &Registration{
		PeerID:           peerID,
		Skills:           copiedSkills,
		AgentName:        agentName,
		AgentDescription: agentDesc,
		RegisteredAt:     registeredAt,
		ExpiresAt:        expiresAt,
		Origin:           originRelayID,
		Version:          version,
	}

	return nil
}

// LocalRegistrations returns local (non-federated) registrations updated since the given time.
func (r *Registry) LocalRegistrations(since time.Time) []Registration {
	r.mu.RLock()
	defer r.mu.RUnlock()

	now := time.Now()
	var results []Registration
	for _, reg := range r.entries {
		if now.After(reg.ExpiresAt) {
			continue
		}
		if reg.RegisteredAt.After(since) || reg.RegisteredAt.Equal(since) {
			results = append(results, *reg)
		}
	}
	return results
}

// FederatedRegistrations returns all non-expired federated registrations.
func (r *Registry) FederatedRegistrations() []Registration {
	r.mu.RLock()
	defer r.mu.RUnlock()

	now := time.Now()
	var results []Registration
	for _, reg := range r.federated {
		if now.Before(reg.ExpiresAt) {
			results = append(results, *reg)
		}
	}
	return results
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
