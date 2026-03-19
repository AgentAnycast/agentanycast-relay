package federation

import (
	"context"
	"sync"
	"time"

	pb "github.com/agentanycast/agentanycast-proto/gen/go/agentanycast/v1"
	"github.com/agentanycast/agentanycast-relay/internal/registry"
	"google.golang.org/protobuf/types/known/timestamppb"
)

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

// syncAll pulls updates from all configured peer relays.
func (f *Federation) syncAll(ctx context.Context) {
	for addr, client := range f.clients {
		if err := f.syncPeer(ctx, addr, client); err != nil {
			f.logger.Warn("federation sync failed",
				"peer_addr", addr,
				"error", err,
			)
		}
	}
}

// syncPeer pulls registration updates from one peer relay.
func (f *Federation) syncPeer(ctx context.Context, addr string, client pb.FederationServiceClient) error {
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
		return err
	}

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

// PushLocal pushes local registration changes to all peer relays.
func (f *Federation) PushLocal(regs []registry.Registration) error {
	if len(f.clients) == 0 || len(regs) == 0 {
		return nil
	}

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
	for addr, client := range f.clients {
		wg.Add(1)
		go func(addr string, client pb.FederationServiceClient) {
			defer wg.Done()
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
