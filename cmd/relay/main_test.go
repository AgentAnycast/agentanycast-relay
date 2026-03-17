package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/multiformats/go-multiaddr"
)

// ── loadOrGenerateKey tests ─────────────────────────────────────────

func TestLoadOrGenerateKey_EmptyPath_GeneratesEphemeralKey(t *testing.T) {
	priv, err := loadOrGenerateKey("")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if priv == nil {
		t.Fatal("expected non-nil private key")
	}
	if priv.Type() != crypto.Ed25519 {
		t.Fatalf("expected Ed25519 key, got %v", priv.Type())
	}
}

func TestLoadOrGenerateKey_EmptyPath_GeneratesUniqueKeys(t *testing.T) {
	priv1, err := loadOrGenerateKey("")
	if err != nil {
		t.Fatalf("key1: %v", err)
	}
	priv2, err := loadOrGenerateKey("")
	if err != nil {
		t.Fatalf("key2: %v", err)
	}

	raw1, _ := crypto.MarshalPrivateKey(priv1)
	raw2, _ := crypto.MarshalPrivateKey(priv2)
	if string(raw1) == string(raw2) {
		t.Fatal("two ephemeral keys should differ")
	}
}

func TestLoadOrGenerateKey_NonExistentPath_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "data", "key")

	priv, err := loadOrGenerateKey(keyPath)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if priv == nil {
		t.Fatal("expected non-nil private key")
	}

	// File should have been created.
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("key file should exist: %v", err)
	}
	// File should have restricted permissions (0600).
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("expected permissions 0600, got %o", perm)
	}
}

func TestLoadOrGenerateKey_ExistingKey_RoundTrips(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "data", "key")

	// First call: generates and persists.
	priv1, err := loadOrGenerateKey(keyPath)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}

	// Second call: should load the same key from disk.
	priv2, err := loadOrGenerateKey(keyPath)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}

	raw1, _ := crypto.MarshalPrivateKey(priv1)
	raw2, _ := crypto.MarshalPrivateKey(priv2)
	if string(raw1) != string(raw2) {
		t.Fatal("reloaded key should match the original")
	}
}

func TestLoadOrGenerateKey_CorruptFile_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key")

	// Write garbage data.
	if err := os.WriteFile(keyPath, []byte("not a valid key"), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	_, err := loadOrGenerateKey(keyPath)
	if err == nil {
		t.Fatal("expected error for corrupt key file")
	}
}

func TestLoadOrGenerateKey_UnreadablePath_ReturnsError(t *testing.T) {
	// Point to a path inside a file (not a directory) so ReadFile fails
	// with something other than IsNotExist.
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// Try to read a "file" inside another file — should fail with ENOTDIR.
	keyPath := filepath.Join(blocker, "key")

	_, err := loadOrGenerateKey(keyPath)
	if err == nil {
		t.Fatal("expected error for unreadable path")
	}
}

// ── QUIC address derivation tests ───────────────────────────────────

func TestQUICAddressDerivation(t *testing.T) {
	tests := []struct {
		name     string
		tcpAddr  string
		wantQUIC string
	}{
		{
			name:     "default listen addr",
			tcpAddr:  "/ip4/0.0.0.0/tcp/4001",
			wantQUIC: "/ip4/0.0.0.0/udp/4001/quic-v1",
		},
		{
			name:     "custom port",
			tcpAddr:  "/ip4/127.0.0.1/tcp/9999",
			wantQUIC: "/ip4/127.0.0.1/udp/9999/quic-v1",
		},
		{
			name:     "ipv6",
			tcpAddr:  "/ip6/::/tcp/4001",
			wantQUIC: "/ip6/::/udp/4001/quic-v1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Replicate the derivation logic from main().
			quicAddrStr := strings.Replace(tc.tcpAddr, "/tcp/", "/udp/", 1) + "/quic-v1"
			if quicAddrStr != tc.wantQUIC {
				t.Fatalf("got %q, want %q", quicAddrStr, tc.wantQUIC)
			}

			// Verify the derived address is a valid multiaddr.
			_, err := multiaddr.NewMultiaddr(quicAddrStr)
			if err != nil {
				t.Fatalf("derived QUIC address is invalid: %v", err)
			}
		})
	}
}

// ── Listen address validation tests ─────────────────────────────────

func TestDefaultListenAddress_IsValid(t *testing.T) {
	defaultAddr := "/ip4/0.0.0.0/tcp/4001"
	_, err := multiaddr.NewMultiaddr(defaultAddr)
	if err != nil {
		t.Fatalf("default listen address should be valid: %v", err)
	}
}

func TestInvalidListenAddress_Fails(t *testing.T) {
	_, err := multiaddr.NewMultiaddr("not-a-multiaddr")
	if err == nil {
		t.Fatal("expected error for invalid multiaddr")
	}
}

// ── Version variable test ───────────────────────────────────────────

func TestVersionDefault(t *testing.T) {
	if version != "dev" {
		t.Fatalf("expected default version %q, got %q", "dev", version)
	}
}
