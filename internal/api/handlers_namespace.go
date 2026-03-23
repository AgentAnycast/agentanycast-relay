package api

import (
	"encoding/json"
	"net/http"
)

type createNamespaceRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

func (s *Server) handleCreateNamespace(w http.ResponseWriter, r *http.Request) {
	var req createNamespaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "namespace name is required", http.StatusBadRequest)
		return
	}

	ns, err := s.namespaceMgr.Create(req.Name, req.Description)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	w.WriteHeader(http.StatusCreated)
	writeJSON(w, ns)
}

func (s *Server) handleListNamespaces(w http.ResponseWriter, _ *http.Request) {
	namespaces := s.namespaceMgr.List()
	writeJSON(w, map[string]any{
		"count":      len(namespaces),
		"namespaces": namespaces,
	})
}

func (s *Server) handleDeleteNamespace(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "namespace name is required", http.StatusBadRequest)
		return
	}

	if err := s.namespaceMgr.Delete(name); err != nil {
		// Distinguish between "cannot delete default" and "not found".
		if name == "default" {
			http.Error(w, err.Error(), http.StatusForbidden)
		} else {
			http.Error(w, err.Error(), http.StatusNotFound)
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
