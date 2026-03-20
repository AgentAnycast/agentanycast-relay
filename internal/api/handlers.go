package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"

	"github.com/agentanycast/agentanycast-relay/internal/registry"
)

// AgentResponse is the JSON representation of a registered agent.
type AgentResponse struct {
	PeerID      string          `json:"peer_id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Skills      []SkillResponse `json:"skills"`
	Origin      string          `json:"origin,omitempty"`
	ExpiresAt   string          `json:"expires_at"`
}

// SkillResponse is the JSON representation of a skill.
type SkillResponse struct {
	ID          string            `json:"id"`
	Description string            `json:"description"`
	Tags        map[string]string `json:"tags,omitempty"`
}

// SkillSummary is the JSON representation of a skill in the skills list.
type SkillSummary struct {
	ID         string `json:"id"`
	AgentCount int    `json:"agent_count"`
}

// StatsResponse is the JSON representation of registry statistics.
type StatsResponse struct {
	Registry   registry.RegistryStats `json:"registry"`
	Federation *FederationStats       `json:"federation,omitempty"`
}

// FederationStats is a subset of federation health for the stats endpoint.
type FederationStats struct {
	Peers        int `json:"peers"`
	HealthyPeers int `json:"healthy_peers"`
}

func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	skillFilter := r.URL.Query().Get("skill")
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	limit := 100
	if limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 && v <= 1000 {
			limit = v
		}
	}
	offset := 0
	if offsetStr != "" {
		if v, err := strconv.Atoi(offsetStr); err == nil && v >= 0 {
			offset = v
		}
	}

	// Collect all matching registrations.
	var regs []registry.Registration
	if skillFilter != "" {
		// Fetch all matching registrations (not limited by offset+limit) so we
		// can report an accurate total count before paginating.
		regs = s.registry.DiscoverBySkill(skillFilter, nil, registry.MaxDiscoverLimit, true)
	} else {
		regs = s.registry.AllRegistrations()
		fedRegs := s.registry.FederatedRegistrations()
		regs = append(regs, fedRegs...)
	}

	// Sort by peer ID for deterministic output.
	sort.Slice(regs, func(i, j int) bool {
		return regs[i].PeerID < regs[j].PeerID
	})

	total := len(regs)

	// Apply pagination.
	if offset >= len(regs) {
		regs = nil
	} else {
		end := offset + limit
		if end > len(regs) {
			end = len(regs)
		}
		regs = regs[offset:end]
	}

	agents := make([]AgentResponse, 0, len(regs))
	for _, reg := range regs {
		agents = append(agents, regToAgent(reg))
	}

	writeJSON(w, map[string]any{
		"total":  total,
		"count":  len(agents),
		"agents": agents,
	})
}

func (s *Server) handleGetAgent(w http.ResponseWriter, r *http.Request) {
	peerID := r.PathValue("peer_id")
	if peerID == "" {
		http.Error(w, "peer_id required", http.StatusBadRequest)
		return
	}

	reg, ok := s.registry.GetRegistration(peerID)
	if !ok {
		http.Error(w, "agent not found", http.StatusNotFound)
		return
	}
	writeJSON(w, regToAgent(reg))
}

func (s *Server) handleListSkills(w http.ResponseWriter, r *http.Request) {
	// Aggregate skills from all registrations.
	skillCounts := make(map[string]int)

	allRegs := s.registry.AllRegistrations()
	fedRegs := s.registry.FederatedRegistrations()
	allRegs = append(allRegs, fedRegs...)

	for _, reg := range allRegs {
		for _, skill := range reg.Skills {
			skillCounts[skill.SkillID]++
		}
	}

	skills := make([]SkillSummary, 0, len(skillCounts))
	for id, count := range skillCounts {
		skills = append(skills, SkillSummary{ID: id, AgentCount: count})
	}

	sort.Slice(skills, func(i, j int) bool {
		return skills[i].ID < skills[j].ID
	})

	writeJSON(w, map[string]any{
		"count":  len(skills),
		"skills": skills,
	})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	resp := StatsResponse{
		Registry: s.registry.Stats(),
	}

	if s.federation != nil {
		fh := s.federation.HealthStatus()
		resp.Federation = &FederationStats{
			Peers:        fh.Peers,
			HealthyPeers: fh.HealthyPeers,
		}
	}

	writeJSON(w, resp)
}

func regToAgent(reg registry.Registration) AgentResponse {
	skills := make([]SkillResponse, 0, len(reg.Skills))
	for _, s := range reg.Skills {
		skills = append(skills, SkillResponse{
			ID:          s.SkillID,
			Description: s.Description,
			Tags:        s.Tags,
		})
	}
	return AgentResponse{
		PeerID:      reg.PeerID,
		Name:        reg.AgentName,
		Description: reg.AgentDescription,
		Skills:      skills,
		Origin:      reg.Origin,
		ExpiresAt:   reg.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}
