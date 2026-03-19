package federation

import (
	"context"
	"log/slog"
	"time"

	pb "github.com/agentanycast/agentanycast-proto/gen/go/agentanycast/v1"
	"github.com/agentanycast/agentanycast-relay/internal/registry"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Server implements the FederationService gRPC interface.
type Server struct {
	pb.UnimplementedFederationServiceServer
	registry *registry.Registry
	relayID  string
	logger   *slog.Logger
}

// NewServer creates a new FederationService gRPC server.
func NewServer(reg *registry.Registry, relayID string, logger *slog.Logger) *Server {
	return &Server{
		registry: reg,
		relayID:  relayID,
		logger:   logger,
	}
}

// SyncRegistrations returns local registrations updated since the requested timestamp.
// This is called by peer relays to pull updates.
func (s *Server) SyncRegistrations(_ context.Context, req *pb.SyncRegistrationsRequest) (*pb.SyncRegistrationsResponse, error) {
	if req.RelayId == "" {
		return nil, status.Error(codes.InvalidArgument, "relay_id is required")
	}

	var since time.Time
	if req.Since != nil {
		since = req.Since.AsTime()
	}

	limit := int(req.Limit)
	if limit <= 0 {
		limit = 500
	}
	if limit > 5000 {
		limit = 5000
	}

	// Return local registrations updated since the given timestamp.
	localRegs := s.registry.LocalRegistrations(since)

	now := time.Now()
	fedRegs := make([]*pb.FederatedRegistration, 0, len(localRegs))
	for _, reg := range localRegs {
		if len(fedRegs) >= limit {
			break
		}

		pbSkills := registrySkillsToPb(reg.Skills)
		fedRegs = append(fedRegs, &pb.FederatedRegistration{
			PeerId:           reg.PeerID,
			OriginRelayId:    s.relayID,
			Skills:           pbSkills,
			AgentName:        reg.AgentName,
			AgentDescription: reg.AgentDescription,
			RegisteredAt:     timestamppb.New(reg.RegisteredAt),
			ExpiresAt:        timestamppb.New(reg.ExpiresAt),
			Version:          reg.Version,
		})
	}

	s.logger.Debug("sync registrations served",
		"requester", req.RelayId,
		"since", since,
		"count", len(fedRegs),
	)

	return &pb.SyncRegistrationsResponse{
		Registrations: fedRegs,
		SyncTimestamp: timestamppb.New(now),
	}, nil
}

// PushRegistrations accepts registration updates pushed by a peer relay.
func (s *Server) PushRegistrations(_ context.Context, req *pb.PushRegistrationsRequest) (*pb.PushRegistrationsResponse, error) {
	if req.RelayId == "" {
		return nil, status.Error(codes.InvalidArgument, "relay_id is required")
	}

	var accepted, rejected int32

	for _, fedReg := range req.Registrations {
		if fedReg.PeerId == "" {
			rejected++
			continue
		}

		skills := pbSkillsToRegistry(fedReg.Skills)
		originRelay := fedReg.OriginRelayId
		if originRelay == "" {
			originRelay = req.RelayId
		}

		var registeredAt, expiresAt time.Time
		if fedReg.RegisteredAt != nil {
			registeredAt = fedReg.RegisteredAt.AsTime()
		}
		if fedReg.ExpiresAt != nil {
			expiresAt = fedReg.ExpiresAt.AsTime()
		}

		err := s.registry.RegisterFederated(
			fedReg.PeerId,
			originRelay,
			skills,
			fedReg.AgentName,
			fedReg.AgentDescription,
			registeredAt,
			expiresAt,
			fedReg.Version,
		)
		if err != nil {
			s.logger.Debug("rejected pushed registration",
				"peer_id", fedReg.PeerId,
				"origin", originRelay,
				"error", err,
			)
			rejected++
			continue
		}
		accepted++
	}

	s.logger.Info("push registrations processed",
		"requester", req.RelayId,
		"accepted", accepted,
		"rejected", rejected,
	)

	return &pb.PushRegistrationsResponse{
		Accepted: accepted,
		Rejected: rejected,
	}, nil
}
