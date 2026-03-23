package api

import (
	"encoding/json"
	"net/http"

	"github.com/agentanycast/agentanycast-relay/internal/policy"
)

func (s *Server) handleCreatePolicy(w http.ResponseWriter, r *http.Request) {
	var rule policy.Rule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if rule.ID == "" {
		http.Error(w, "rule id is required", http.StatusBadRequest)
		return
	}
	if rule.Effect == "" {
		http.Error(w, "rule effect is required", http.StatusBadRequest)
		return
	}

	if err := s.policyEngine.AddRule(rule); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	w.WriteHeader(http.StatusCreated)
	writeJSON(w, rule)
}

func (s *Server) handleListPolicies(w http.ResponseWriter, _ *http.Request) {
	rules := s.policyEngine.ListRules()
	writeJSON(w, map[string]any{
		"count": len(rules),
		"rules": rules,
	})
}

func (s *Server) handleUpdatePolicy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "policy id is required", http.StatusBadRequest)
		return
	}

	var rule policy.Rule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	rule.ID = id

	if err := s.policyEngine.UpdateRule(rule); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	writeJSON(w, rule)
}

func (s *Server) handleDeletePolicy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "policy id is required", http.StatusBadRequest)
		return
	}

	if err := s.policyEngine.RemoveRule(id); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
