// Package namespace provides multi-tenant namespace isolation for the relay.
package namespace

import (
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"
)

const defaultNamespace = "default"

// Namespace represents an isolated tenant namespace.
type Namespace struct {
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// Manager manages namespace lifecycle.
type Manager struct {
	mu         sync.RWMutex
	namespaces map[string]*Namespace
	logger     *slog.Logger
}

// NewManager creates a new namespace manager with the "default" namespace pre-created.
func NewManager(logger *slog.Logger) *Manager {
	m := &Manager{
		namespaces: make(map[string]*Namespace),
		logger:     logger,
	}
	m.namespaces[defaultNamespace] = &Namespace{
		Name:      defaultNamespace,
		CreatedAt: time.Now(),
	}
	return m
}

// Create creates a new namespace. Returns an error if the namespace already exists.
func (m *Manager) Create(name, description string) (*Namespace, error) {
	if name == "" {
		return nil, fmt.Errorf("namespace name cannot be empty")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.namespaces[name]; exists {
		return nil, fmt.Errorf("namespace %q already exists", name)
	}

	ns := &Namespace{
		Name:        name,
		Description: description,
		CreatedAt:   time.Now(),
	}
	m.namespaces[name] = ns
	m.logger.Info("namespace created", "name", name)
	return ns, nil
}

// Get retrieves a namespace by name.
func (m *Manager) Get(name string) (*Namespace, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ns, ok := m.namespaces[name]
	if !ok {
		return nil, false
	}
	// Return a copy.
	cp := *ns
	return &cp, true
}

// List returns all namespaces sorted by name.
func (m *Manager) List() []Namespace {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]Namespace, 0, len(m.namespaces))
	for _, ns := range m.namespaces {
		result = append(result, *ns)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// Delete removes a namespace. The "default" namespace cannot be deleted.
func (m *Manager) Delete(name string) error {
	if name == defaultNamespace {
		return fmt.Errorf("cannot delete the %q namespace", defaultNamespace)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.namespaces[name]; !exists {
		return fmt.Errorf("namespace %q not found", name)
	}

	delete(m.namespaces, name)
	m.logger.Info("namespace deleted", "name", name)
	return nil
}
