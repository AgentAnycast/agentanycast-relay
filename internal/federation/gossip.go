package federation

import (
	"context"
	"math"
	"sync"
	"time"

	pb "github.com/agentanycast/agentanycast-proto/gen/go/agentanycast/v1"
	"github.com/agentanycast/agentanycast-relay/internal/registry"
	"github.com/agentanycast/agentanycast-relay/internal/telemetry"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Prometheus metrics for federation operations.
var (
	metricSyncDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "federation_sync_duration_seconds",
		Help:    "Duration of federation sync per peer.",
		Buckets: prometheus.DefBuckets,
	}, []string{"peer"})
	metricSyncErrorsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "federation_sync_errors_total",
		Help: "Total number of federation sync errors.",
	})
	metricPushDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "federation_push_duration_seconds",
		Help:    "Duration of federation push operations.",
		Buckets: prometheus.DefBuckets,
	})
)

// Circuit breaker constants.
const (
	circuitBreakerBaseDelay = 5 * time.Second
	circuitBreakerMaxDelay  = 5 * time.Minute
)

// maxPushConcurrency is the maximum number of concurrent push goroutines.
const maxPushConcurrency = 32

// peerState tracks per-peer failure state for circuit breaking.
type peerState struct {
	consecutiveFailures int
	backoffUntil        time.Time
}

// syncLoop runs periodically, pulling updates from all peer relays.
func (f *Federation) syncLoop(ctx context.Context) {
	defer f.wg.Done()

	ticker := time.NewTicker(f.cfg.SyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			f.syncAll(ctx)
		}
	}
}

// syncAll pulls updates from all configured peer relays in parallel.
func (f *Federation) syncAll(ctx context.Context) {
	f.mu.RLock()
	clients := make(map[string]pb.FederationServiceClient, len(f.clients))
	for k, v := range f.clients {
		clients[k] = v
	}
	f.mu.RUnlock()

	var wg sync.WaitGroup
	for addr, client := range clients {
		wg.Add(1)
		go func(addr string, client pb.FederationServiceClient) {
			defer wg.Done()
			if err := f.syncPeer(ctx, addr, client); err != nil {
				f.logger.Warn("federation sync failed",
					"peer_addr", addr,
					"error", err,
				)
			}
		}(addr, client)
	}
	wg.Wait()
}

// syncPeer pulls registration updates from one peer relay.
// Respects circuit breaker backoff per peer.
func (f *Federation) syncPeer(ctx context.Context, addr string, client pb.FederationServiceClient) error {
	tracer := telemetry.Tracer("agentanycast.federation")
	ctx, span := tracer.Start(ctx, "federation.sync_peer")
	span.SetAttributes(attribute.String("peer_addr", addr))
	defer span.End()

	start := time.Now()
	defer func() {
		metricSyncDuration.WithLabelValues(addr).Observe(time.Since(start).Seconds())
	}()

	// Circuit breaker: check if this peer is in backoff.
	f.mu.RLock()
	state := f.peerStates[addr]
	f.mu.RUnlock()

	if time.Now().Before(state.backoffUntil) {
		f.logger.Debug("skipping peer sync (circuit breaker backoff)",
			"peer_addr", addr,
			"backoff_until", state.backoffUntil,
		)
		return nil
	}

	f.mu.RLock()
	since := f.lastSync[addr]
	f.mu.RUnlock()

	var sincePb *timestamppb.Timestamp
	if !since.IsZero() {
		sincePb = timestamppb.New(since)
	}

	resp, err := client.SyncRegistrations(ctx, &pb.SyncRegistrationsRequest{
		RelayId: f.cfg.RelayID,
		Since:   sincePb,
		Limit:   1000,
	})
	if err != nil {
		metricSyncErrorsTotal.Inc()
		f.recordPeerFailure(addr)
		return err
	}

	// Success: reset circuit breaker.
	f.recordPeerSuccess(addr)

	accepted := 0
	for _, fedReg := range resp.Registrations {
		// Guard against nil timestamps to prevent panic on .AsTime().
		if fedReg.RegisteredAt == nil || fedReg.ExpiresAt == nil {
			f.logger.Debug("skipping federated registration with nil timestamp",
				"peer_id", fedReg.PeerId,
			)
			continue
		}

		skills := pbSkillsToRegistry(fedReg.Skills)
		originRelay := fedReg.OriginRelayId
		if originRelay == "" {
			originRelay = addr // infer origin from the peer we synced from
		}

		err := f.registry.RegisterFederated(
			fedReg.PeerId,
			originRelay,
			skills,
			fedReg.AgentName,
			fedReg.AgentDescription,
			fedReg.RegisteredAt.AsTime(),
			fedReg.ExpiresAt.AsTime(),
			fedReg.Version,
		)
		if err != nil {
			f.logger.Debug("rejected federated registration",
				"peer_id", fedReg.PeerId,
				"error", err,
			)
			continue
		}
		accepted++
	}

	// Update last sync timestamp.
	if resp.SyncTimestamp != nil {
		f.mu.Lock()
		f.lastSync[addr] = resp.SyncTimestamp.AsTime()
		f.mu.Unlock()
	}

	if accepted > 0 {
		f.logger.Info("federation sync completed",
			"peer_addr", addr,
			"received", len(resp.Registrations),
			"accepted", accepted,
		)
	}

	return nil
}

// recordPeerFailure increments the circuit breaker failure counter and sets backoff.
func (f *Federation) recordPeerFailure(addr string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	state := f.peerStates[addr]
	state.consecutiveFailures++
	delay := circuitBreakerBaseDelay * time.Duration(math.Pow(2, float64(state.consecutiveFailures-1)))
	if delay > circuitBreakerMaxDelay {
		delay = circuitBreakerMaxDelay
	}
	state.backoffUntil = time.Now().Add(delay)
	f.peerStates[addr] = state
}

// recordPeerSuccess resets the circuit breaker state for a peer.
func (f *Federation) recordPeerSuccess(addr string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, exists := f.peerStates[addr]; exists {
		f.peerStates[addr] = peerState{}
	}
}

// PushLocal pushes local registration changes to all peer relays.
// Uses bounded concurrency to limit simultaneous outbound pushes.
func (f *Federation) PushLocal(regs []registry.Registration) error {
	f.mu.RLock()
	clients := make(map[string]pb.FederationServiceClient, len(f.clients))
	for k, v := range f.clients {
		clients[k] = v
	}
	f.mu.RUnlock()

	if len(clients) == 0 || len(regs) == 0 {
		return nil
	}

	start := time.Now()
	defer func() {
		metricPushDuration.Observe(time.Since(start).Seconds())
	}()

	fedRegs := make([]*pb.FederatedRegistration, 0, len(regs))
	for _, reg := range regs {
		pbSkills := registrySkillsToPb(reg.Skills)
		fedRegs = append(fedRegs, &pb.FederatedRegistration{
			PeerId:           reg.PeerID,
			OriginRelayId:    f.cfg.RelayID,
			Skills:           pbSkills,
			AgentName:        reg.AgentName,
			AgentDescription: reg.AgentDescription,
			RegisteredAt:     timestamppb.New(reg.RegisteredAt),
			ExpiresAt:        timestamppb.New(reg.ExpiresAt),
			Version:          reg.Version,
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var (
		mu       sync.Mutex
		firstErr error
		wg       sync.WaitGroup
	)
	sem := make(chan struct{}, maxPushConcurrency) // bounded concurrency
	for addr, client := range clients {
		wg.Add(1)
		go func(addr string, client pb.FederationServiceClient) {
			defer wg.Done()
			sem <- struct{}{}        // acquire
			defer func() { <-sem }() // release

			resp, err := client.PushRegistrations(ctx, &pb.PushRegistrationsRequest{
				RelayId:       f.cfg.RelayID,
				Registrations: fedRegs,
			})
			if err != nil {
				f.logger.Warn("federation push failed",
					"peer_addr", addr,
					"error", err,
				)
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				return
			}
			f.logger.Debug("federation push completed",
				"peer_addr", addr,
				"accepted", resp.Accepted,
				"rejected", resp.Rejected,
			)
		}(addr, client)
	}
	wg.Wait()

	return firstErr
}

// pbSkillsToRegistry converts protobuf SkillInfo to registry SkillInfo.
func pbSkillsToRegistry(pbSkills []*pb.SkillInfo) []registry.SkillInfo {
	skills := make([]registry.SkillInfo, 0, len(pbSkills))
	for _, s := range pbSkills {
		skills = append(skills, registry.SkillInfo{
			SkillID:     s.SkillId,
			Description: s.Description,
			Tags:        s.Tags,
		})
	}
	return skills
}

// registrySkillsToPb converts registry SkillInfo to protobuf SkillInfo.
func registrySkillsToPb(skills []registry.SkillInfo) []*pb.SkillInfo {
	pbSkills := make([]*pb.SkillInfo, 0, len(skills))
	for _, s := range skills {
		pbSkills = append(pbSkills, &pb.SkillInfo{
			SkillId:     s.SkillID,
			Description: s.Description,
			Tags:        s.Tags,
		})
	}
	return pbSkills
}
