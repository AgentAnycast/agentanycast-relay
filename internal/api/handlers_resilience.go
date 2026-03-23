package api

import (
	"net/http"
)

func (s *Server) handleListBreakers(w http.ResponseWriter, _ *http.Request) {
	statuses := s.breakerRegistry.List()
	writeJSON(w, map[string]any{
		"count":            len(statuses),
		"circuit_breakers": statuses,
	})
}

func (s *Server) handleResetBreaker(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "circuit breaker name is required", http.StatusBadRequest)
		return
	}

	cb := s.breakerRegistry.Get(name)
	cb.Reset()

	writeJSON(w, map[string]any{
		"name":  name,
		"state": cb.GetState(),
	})
}
