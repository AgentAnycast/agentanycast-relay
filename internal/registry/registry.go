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
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
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

// ErrFederatedRegistryFull is returned when the federated registry has reached its maximum capacity.
var ErrFederatedRegistryFull = errors.New("federated registry has reached maximum capacity")

// Prometheus metrics for registry operations.
var (
	metricLocalCount = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "registry_local_count",
		Help: "Number of active local registrations.",
	})
	metricFederatedCount = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "registry_federated_count",
		Help: "Number of active federated registrations.",
	})
	metricDiscoverDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "registry_discover_duration_seconds",
		Help:    "Duration of discovery queries.",
		Buckets: prometheus.DefBuckets,
	})
	metricEvictionsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "registry_evictions_total",
		Help: "Total number of expired registrations evicted.",
	})
)

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
	TTL                       time.Duration
	MaxRegistrations          int
	MaxFederatedRegistrations int
	CleanupInterval           time.Duration
	Logger                    *slog.Logger
}

// Registry is a thread-safe, in-memory skill registry.
type Registry struct {
	mu        sync.RWMutex
	entries   map[string]*Registration // keyed by peer_id (local registrations)
	federated map[string]*Registration // keyed by peer_id (federated registrations)

	// Inverted indexes: skillID -> set of peerIDs for O(1) discovery.
	skillIndex    map[string]map[string]struct{} // local registrations
	fedSkillIndex map[string]map[string]struct{} // federated registrations

	// Atomic counters for O(1) Count().
	localCount     atomic.Int64
	federatedCount atomic.Int64

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
		entries:       make(map[string]*Registration),
		federated:     make(map[string]*Registration),
		skillIndex:    make(map[string]map[string]struct{}),
		fedSkillIndex: make(map[string]map[string]struct{}),
		config:        cfg,
		logger:        cfg.Logger,
		stopCh:        make(chan struct{}),
	}

	go r.cleanupLoop()
	return r
}

// copySkills deep-copies a slice of SkillInfo to avoid referencing caller's maps.
func copySkills(skills []SkillInfo) []SkillInfo {
	copied := make([]SkillInfo, len(skills))
	for i, s := range skills {
		var tags map[string]string
		if len(s.Tags) > 0 {
			tags = make(map[string]string, len(s.Tags))
			for k, v := range s.Tags {
				tags[k] = v
			}
		}
		copied[i] = SkillInfo{
			SkillID:     s.SkillID,
			Description: s.Description,
			Tags:        tags,
		}
	}
	return copied
}

// addToSkillIndex adds all skills for a peer to the given inverted index.
func addToSkillIndex(index map[string]map[string]struct{}, peerID string, skills []SkillInfo) {
	for _, s := range skills {
		peers, ok := index[s.SkillID]
		if !ok {
			peers = make(map[string]struct{})
			index[s.SkillID] = peers
		}
		peers[peerID] = struct{}{}
	}
}

// removeFromSkillIndex removes all skills for a peer from the given inverted index.
func removeFromSkillIndex(index map[string]map[string]struct{}, peerID string, skills []SkillInfo) {
	for _, s := range skills {
		if peers, ok := index[s.SkillID]; ok {
			delete(peers, peerID)
			if len(peers) == 0 {
				delete(index, s.SkillID)
			}
		}
	}
}

// Register adds or refreshes an agent's skill registration.
// Returns the expiration time and an error if the registry is full.
func (r *Registry) Register(peerID string, skills []SkillInfo, agentName, agentDesc string) (time.Time, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Enforce capacity limit (allow updates to existing registrations).
	existing, exists := r.entries[peerID]
	if !exists {
		if len(r.entries) >= r.config.MaxRegistrations {
			return time.Time{}, ErrRegistryFull
		}
	}

	now := time.Now()
	expiresAt := now.Add(r.config.TTL)
	copiedSkills := copySkills(skills)

	// Update inverted index: remove old skills, add new ones.
	if exists {
		removeFromSkillIndex(r.skillIndex, peerID, existing.Skills)
	}
	addToSkillIndex(r.skillIndex, peerID, copiedSkills)

	r.entries[peerID] = &Registration{
		PeerID:           peerID,
		Skills:           copiedSkills,
		AgentName:        agentName,
		AgentDescription: agentDesc,
		RegisteredAt:     now,
		ExpiresAt:        expiresAt,
		Version:          uint64(now.UnixNano()),
	}

	if !exists {
		r.localCount.Add(1)
		metricLocalCount.Set(float64(r.localCount.Load()))
	}

	return expiresAt, nil
}

// Unregister removes specific skills for a peer. If skillIDs is empty,
// removes the entire registration.
func (r *Registry) Unregister(peerID string, skillIDs []string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(skillIDs) == 0 {
		if reg, ok := r.entries[peerID]; ok {
			removeFromSkillIndex(r.skillIndex, peerID, reg.Skills)
			delete(r.entries, peerID)
			r.localCount.Add(-1)
			metricLocalCount.Set(float64(r.localCount.Load()))
		}
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

	// Remove from inverted index for the specific skills being unregistered.
	for _, s := range reg.Skills {
		if _, found := remove[s.SkillID]; found {
			if peers, ok := r.skillIndex[s.SkillID]; ok {
				delete(peers, peerID)
				if len(peers) == 0 {
					delete(r.skillIndex, s.SkillID)
				}
			}
		}
	}

	filtered := make([]SkillInfo, 0, len(reg.Skills))
	for _, s := range reg.Skills {
		if _, found := remove[s.SkillID]; !found {
			filtered = append(filtered, s)
		}
	}

	if len(filtered) == 0 {
		delete(r.entries, peerID)
		r.localCount.Add(-1)
		metricLocalCount.Set(float64(r.localCount.Load()))
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

	start := time.Now()
	defer func() {
		metricDiscoverDuration.Observe(time.Since(start).Seconds())
	}()

	r.mu.RLock()
	defer r.mu.RUnlock()

	now := time.Now()
	var results []Registration

	// Use inverted index for O(1) lookup of peers offering this skill.
	localPeers := make(map[string]struct{})
	if peerIDs, ok := r.skillIndex[skillID]; ok {
		for peerID := range peerIDs {
			reg := r.entries[peerID]
			if reg == nil || now.After(reg.ExpiresAt) {
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
	}

	// Optionally include federated entries, skipping peers already found locally.
	if includeFederated {
		if fedPeerIDs, ok := r.fedSkillIndex[skillID]; ok {
			for peerID := range fedPeerIDs {
				if _, isLocal := localPeers[peerID]; isLocal {
					continue // local registration takes priority
				}
				reg := r.federated[peerID]
				if reg == nil || now.After(reg.ExpiresAt) {
					continue
				}
				if hasSkill(reg, skillID, tags) {
					results = append(results, *reg)
					if len(results) >= limit {
						break
					}
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
	if reg, ok := r.entries[peerID]; ok {
		removeFromSkillIndex(r.skillIndex, peerID, reg.Skills)
		delete(r.entries, peerID)
		r.localCount.Add(-1)
		metricLocalCount.Set(float64(r.localCount.Load()))
		r.logger.Info("peer removed from registry (disconnect)", "peer_id", peerID)
	}
}

// Count returns the number of active local registrations.
func (r *Registry) Count() int {
	return int(r.localCount.Load())
}

// FederatedCount returns the number of active federated registrations.
func (r *Registry) FederatedCount() int {
	return int(r.federatedCount.Load())
}

// RegistryStats holds a snapshot of registry statistics.
type RegistryStats struct {
	LocalCount     int `json:"local_count"`
	FederatedCount int `json:"federated_count"`
	SkillCount     int `json:"skill_count"`
	FedSkillCount  int `json:"fed_skill_count"`
}

// Stats returns a point-in-time snapshot of registry statistics.
func (r *Registry) Stats() RegistryStats {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return RegistryStats{
		LocalCount:     int(r.localCount.Load()),
		FederatedCount: int(r.federatedCount.Load()),
		SkillCount:     len(r.skillIndex),
		FedSkillCount:  len(r.fedSkillIndex),
	}
}

// GetRegistration returns the registration for a given peer ID, checking both
// local and federated entries. Returns the registration and true if found and
// not expired, or a zero value and false otherwise.
func (r *Registry) GetRegistration(peerID string) (Registration, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	now := time.Now()
	if reg, ok := r.entries[peerID]; ok && now.Before(reg.ExpiresAt) {
		return *reg, true
	}
	if reg, ok := r.federated[peerID]; ok && now.Before(reg.ExpiresAt) {
		return *reg, true
	}
	return Registration{}, false
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
			removeFromSkillIndex(r.skillIndex, peerID, reg.Skills)
			delete(r.entries, peerID)
			r.localCount.Add(-1)
			evicted++
		}
	}
	fedEvicted := 0
	for peerID, reg := range r.federated {
		if now.After(reg.ExpiresAt) {
			removeFromSkillIndex(r.fedSkillIndex, peerID, reg.Skills)
			delete(r.federated, peerID)
			r.federatedCount.Add(-1)
			fedEvicted++
		}
	}

	totalEvicted := evicted + fedEvicted
	if totalEvicted > 0 {
		metricEvictionsTotal.Add(float64(totalEvicted))
		metricLocalCount.Set(float64(r.localCount.Load()))
		metricFederatedCount.Set(float64(r.federatedCount.Load()))
		r.logger.Debug("evicted expired registrations",
			"local", evicted,
			"federated", fedEvicted,
			"remaining_local", len(r.entries),
			"remaining_federated", len(r.federated),
		)
	}
}

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

	copiedSkills := copySkills(skills)

	// Update federated inverted index.
	if existing, ok := r.federated[peerID]; ok {
		removeFromSkillIndex(r.fedSkillIndex, peerID, existing.Skills)
	} else {
		r.federatedCount.Add(1)
		metricFederatedCount.Set(float64(r.federatedCount.Load()))
	}
	addToSkillIndex(r.fedSkillIndex, peerID, copiedSkills)

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
