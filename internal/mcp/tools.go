package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/mark3labs/mcp-go/mcp"
)

// handleDiscoverAgents queries the skill registry for agents offering a specific skill.
func (s *Server) handleDiscoverAgents(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	skillID, err := req.RequireString("skill_id")
	if err != nil {
		return mcp.NewToolResultError("skill_id is required"), nil
	}

	limit := req.GetInt("limit", 20)
	if limit > 100 {
		limit = 100
	}

	regs := s.registry.DiscoverBySkill(skillID, nil, limit, true)

	type agentResult struct {
		PeerID      string   `json:"peer_id"`
		AgentName   string   `json:"agent_name"`
		Description string   `json:"description,omitempty"`
		Skills      []string `json:"skills"`
	}

	results := make([]agentResult, 0, len(regs))
	for _, reg := range regs {
		skills := make([]string, 0, len(reg.Skills))
		for _, sk := range reg.Skills {
			skills = append(skills, sk.SkillID)
		}
		results = append(results, agentResult{
			PeerID:      reg.PeerID,
			AgentName:   reg.AgentName,
			Description: reg.AgentDescription,
			Skills:      skills,
		})
	}

	s.logger.Debug("MCP discover_agents",
		"skill_id", skillID,
		"results", len(results),
	)

	if len(results) == 0 {
		return mcp.NewToolResultText(fmt.Sprintf("No agents found offering skill %q. Agents need to register with the relay first.", skillID)), nil
	}

	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return mcp.NewToolResultError("failed to serialize results"), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Found %d agent(s) offering skill %q:\n\n%s", len(results), skillID, string(data))), nil
}

// handleListSkills lists all unique skills registered on the network.
func (s *Server) handleListSkills(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	allRegs := s.registry.AllRegistrations()

	type skillSummary struct {
		SkillID     string `json:"skill_id"`
		Description string `json:"description,omitempty"`
		AgentCount  int    `json:"agent_count"`
	}

	skillMap := make(map[string]*skillSummary)
	for _, reg := range allRegs {
		for _, sk := range reg.Skills {
			if existing, ok := skillMap[sk.SkillID]; ok {
				existing.AgentCount++
			} else {
				skillMap[sk.SkillID] = &skillSummary{
					SkillID:     sk.SkillID,
					Description: sk.Description,
					AgentCount:  1,
				}
			}
		}
	}

	skills := make([]skillSummary, 0, len(skillMap))
	for _, sk := range skillMap {
		skills = append(skills, *sk)
	}

	s.logger.Debug("MCP list_skills", "unique_skills", len(skills))

	if len(skills) == 0 {
		return mcp.NewToolResultText("No skills are currently registered on the network. Agents need to connect to the relay and register their skills first."), nil
	}

	data, err := json.MarshalIndent(skills, "", "  ")
	if err != nil {
		return mcp.NewToolResultError("failed to serialize results"), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("%d skill(s) available on the network:\n\n%s", len(skills), string(data))), nil
}

// handleGetRelayInfo returns information about the relay node.
func (s *Server) handleGetRelayInfo(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	peerID := s.host.ID().String()
	addrs := s.host.Addrs()
	addrStrs := make([]string, len(addrs))
	for i, a := range addrs {
		addrStrs[i] = a.String()
	}

	connectedPeers := 0
	for _, p := range s.host.Network().Peers() {
		if s.host.Network().Connectedness(p) == network.Connected {
			connectedPeers++
		}
	}

	info := struct {
		PeerID         string   `json:"peer_id"`
		ListenAddrs    []string `json:"listen_addresses"`
		ConnectedPeers int      `json:"connected_peers"`
		RegisteredAgts int      `json:"registered_agents"`
	}{
		PeerID:         peerID,
		ListenAddrs:    addrStrs,
		ConnectedPeers: connectedPeers,
		RegisteredAgts: s.registry.Count(),
	}

	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return mcp.NewToolResultError("failed to serialize relay info"), nil
	}

	return mcp.NewToolResultText(string(data)), nil
}
