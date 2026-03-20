package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/agentanycast/agentanycast-relay/internal/registry"
)

func setupTestServer(t *testing.T) (*Server, *registry.Registry) {
	t.Helper()
	reg := registry.New(registry.Config{TTL: 30 * time.Second})
	t.Cleanup(reg.Close)

	_, _ = reg.Register("peer-1", []registry.SkillInfo{
		{SkillID: "summarize", Description: "Summarize text"},
		{SkillID: "translate", Description: "Translate text"},
	}, "Agent Alpha", "First agent")
	_, _ = reg.Register("peer-2", []registry.SkillInfo{
		{SkillID: "summarize", Description: "Summarize text"},
	}, "Agent Beta", "Second agent")

	s := &Server{registry: reg}
	return s, reg
}

func TestListAgents(t *testing.T) {
	s, _ := setupTestServer(t)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/agents", s.handleListAgents)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	count := int(resp["count"].(float64))
	if count != 2 {
		t.Fatalf("expected 2 agents, got %d", count)
	}
}

func TestListAgentsWithSkillFilter(t *testing.T) {
	s, _ := setupTestServer(t)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/agents", s.handleListAgents)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents?skill=translate", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	count := int(resp["count"].(float64))
	if count != 1 {
		t.Fatalf("expected 1 agent with translate, got %d", count)
	}
}

func TestGetAgent(t *testing.T) {
	s, _ := setupTestServer(t)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/agents/{peer_id}", s.handleGetAgent)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/peer-1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp AgentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.PeerID != "peer-1" {
		t.Fatalf("expected peer-1, got %s", resp.PeerID)
	}
	if resp.Name != "Agent Alpha" {
		t.Fatalf("expected Agent Alpha, got %s", resp.Name)
	}
	if len(resp.Skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(resp.Skills))
	}
}

func TestGetAgentNotFound(t *testing.T) {
	s, _ := setupTestServer(t)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/agents/{peer_id}", s.handleGetAgent)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/nonexistent", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestListSkills(t *testing.T) {
	s, _ := setupTestServer(t)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/skills", s.handleListSkills)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/skills", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	count := int(resp["count"].(float64))
	if count != 2 {
		t.Fatalf("expected 2 skills, got %d", count)
	}

	skills := resp["skills"].([]any)
	for _, s := range skills {
		skill := s.(map[string]any)
		if skill["id"] == "summarize" {
			if int(skill["agent_count"].(float64)) != 2 {
				t.Fatalf("expected 2 agents for summarize, got %v", skill["agent_count"])
			}
		}
	}
}

func TestStats(t *testing.T) {
	s, _ := setupTestServer(t)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/stats", s.handleStats)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var resp StatsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.Registry.LocalCount != 2 {
		t.Fatalf("expected 2 local, got %d", resp.Registry.LocalCount)
	}
	if resp.Registry.SkillCount != 2 {
		t.Fatalf("expected 2 skills, got %d", resp.Registry.SkillCount)
	}
}
