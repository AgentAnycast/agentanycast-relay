package registry

import (
	"context"
	"errors"
	"log/slog"

	pb "github.com/agentanycast/agentanycast-proto/gen/go/agentanycast/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// GRPCServer implements the RegistryService gRPC interface.
type GRPCServer struct {
	pb.UnimplementedRegistryServiceServer
	registry *Registry
	logger   *slog.Logger
}

// NewGRPCServer creates a new gRPC server backed by the given registry.
func NewGRPCServer(reg *Registry, logger *slog.Logger) *GRPCServer {
	return &GRPCServer{
		registry: reg,
		logger:   logger,
	}
}

func (s *GRPCServer) RegisterSkills(_ context.Context, req *pb.RegisterSkillsRequest) (*pb.RegisterSkillsResponse, error) {
	if req.PeerId == "" {
		return nil, status.Error(codes.InvalidArgument, "peer_id is required")
	}
	if len(req.Skills) == 0 {
		return nil, status.Error(codes.InvalidArgument, "at least one skill is required")
	}

	skills := make([]SkillInfo, 0, len(req.Skills))
	for _, sk := range req.Skills {
		if sk.SkillId == "" {
			return nil, status.Error(codes.InvalidArgument, "skill_id is required for all skills")
		}
		skills = append(skills, SkillInfo{
			SkillID:     sk.SkillId,
			Description: sk.Description,
			Tags:        sk.Tags,
		})
	}

	expiresAt, err := s.registry.Register(req.PeerId, skills, req.AgentName, req.AgentDescription)
	if err != nil {
		if errors.Is(err, ErrRegistryFull) {
			return nil, status.Error(codes.ResourceExhausted, "registry is at maximum capacity")
		}
		return nil, status.Errorf(codes.Internal, "register: %v", err)
	}

	s.logger.Info("skills registered",
		"peer_id", req.PeerId,
		"agent_name", req.AgentName,
		"skill_count", len(skills),
		"expires_at", expiresAt,
	)

	return &pb.RegisterSkillsResponse{
		ExpiresAt: timestamppb.New(expiresAt),
	}, nil
}

func (s *GRPCServer) UnregisterSkills(_ context.Context, req *pb.UnregisterSkillsRequest) (*pb.UnregisterSkillsResponse, error) {
	if req.PeerId == "" {
		return nil, status.Error(codes.InvalidArgument, "peer_id is required")
	}

	s.registry.Unregister(req.PeerId, req.SkillIds)

	return &pb.UnregisterSkillsResponse{}, nil
}

func (s *GRPCServer) DiscoverBySkill(_ context.Context, req *pb.DiscoverBySkillRequest) (*pb.DiscoverBySkillResponse, error) {
	if req.SkillId == "" {
		return nil, status.Error(codes.InvalidArgument, "skill_id is required")
	}

	results := s.registry.DiscoverBySkill(req.SkillId, req.Tags, int(req.Limit))

	agents := make([]*pb.SkillRegistration, 0, len(results))
	for _, r := range results {
		pbSkills := make([]*pb.SkillInfo, 0, len(r.Skills))
		for _, sk := range r.Skills {
			pbSkills = append(pbSkills, &pb.SkillInfo{
				SkillId:     sk.SkillID,
				Description: sk.Description,
				Tags:        sk.Tags,
			})
		}
		agents = append(agents, &pb.SkillRegistration{
			PeerId:           r.PeerID,
			Skills:           pbSkills,
			AgentName:        r.AgentName,
			AgentDescription: r.AgentDescription,
			RegisteredAt:     timestamppb.New(r.RegisteredAt),
			ExpiresAt:        timestamppb.New(r.ExpiresAt),
		})
	}

	s.logger.Debug("discover by skill",
		"skill_id", req.SkillId,
		"results", len(agents),
	)

	return &pb.DiscoverBySkillResponse{Agents: agents}, nil
}

func (s *GRPCServer) Heartbeat(_ context.Context, req *pb.HeartbeatRequest) (*pb.HeartbeatResponse, error) {
	if req.PeerId == "" {
		return nil, status.Error(codes.InvalidArgument, "peer_id is required")
	}

	expiresAt := s.registry.Heartbeat(req.PeerId)
	if expiresAt.IsZero() {
		return nil, status.Errorf(codes.NotFound, "peer %s not found in registry", req.PeerId)
	}

	return &pb.HeartbeatResponse{
		ExpiresAt: timestamppb.New(expiresAt),
	}, nil
}
