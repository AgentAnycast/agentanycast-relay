package namespace

import (
	"log/slog"
	"os"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestManager_DefaultNamespaceExists(t *testing.T) {
	m := NewManager(testLogger())
	ns, ok := m.Get("default")
	if !ok {
		t.Fatal("default namespace should exist")
	}
	if ns.Name != "default" {
		t.Errorf("expected name 'default', got %q", ns.Name)
	}
}

func TestManager_Create(t *testing.T) {
	m := NewManager(testLogger())
	ns, err := m.Create("production", "Production environment")
	if err != nil {
		t.Fatal(err)
	}
	if ns.Name != "production" {
		t.Errorf("expected name 'production', got %q", ns.Name)
	}
	if ns.Description != "Production environment" {
		t.Errorf("expected description 'Production environment', got %q", ns.Description)
	}
	if ns.CreatedAt.IsZero() {
		t.Error("expected non-zero created_at")
	}
}

func TestManager_CreateDuplicate(t *testing.T) {
	m := NewManager(testLogger())
	_, _ = m.Create("staging", "")
	_, err := m.Create("staging", "")
	if err == nil {
		t.Error("expected error creating duplicate namespace")
	}
}

func TestManager_CreateEmpty(t *testing.T) {
	m := NewManager(testLogger())
	_, err := m.Create("", "")
	if err == nil {
		t.Error("expected error creating namespace with empty name")
	}
}

func TestManager_CreateDefaultDuplicate(t *testing.T) {
	m := NewManager(testLogger())
	_, err := m.Create("default", "")
	if err == nil {
		t.Error("expected error creating duplicate 'default' namespace")
	}
}

func TestManager_List(t *testing.T) {
	m := NewManager(testLogger())
	_, _ = m.Create("beta", "")
	_, _ = m.Create("alpha", "")

	list := m.List()
	if len(list) != 3 { // default + alpha + beta
		t.Fatalf("expected 3 namespaces, got %d", len(list))
	}
	// Should be sorted by name.
	if list[0].Name != "alpha" || list[1].Name != "beta" || list[2].Name != "default" {
		t.Errorf("unexpected order: %v", list)
	}
}

func TestManager_Delete(t *testing.T) {
	m := NewManager(testLogger())
	_, _ = m.Create("staging", "")

	err := m.Delete("staging")
	if err != nil {
		t.Fatal(err)
	}

	_, ok := m.Get("staging")
	if ok {
		t.Error("expected namespace to be deleted")
	}
}

func TestManager_DeleteDefault(t *testing.T) {
	m := NewManager(testLogger())
	err := m.Delete("default")
	if err == nil {
		t.Error("expected error deleting default namespace")
	}
}

func TestManager_DeleteNonExistent(t *testing.T) {
	m := NewManager(testLogger())
	err := m.Delete("nonexistent")
	if err == nil {
		t.Error("expected error deleting non-existent namespace")
	}
}

func TestManager_GetNonExistent(t *testing.T) {
	m := NewManager(testLogger())
	_, ok := m.Get("nonexistent")
	if ok {
		t.Error("expected false for non-existent namespace")
	}
}
