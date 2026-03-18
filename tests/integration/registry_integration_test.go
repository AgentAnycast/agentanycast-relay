// Package integration provides integration tests that start a real relay process
// and verify gRPC registry operations end-to-end.
//
// These tests are skipped with -short flag:
//
//	go test ./...               # runs integration tests
//	go test -short ./...        # skips integration tests
package integration

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	pb "github.com/agentanycast/agentanycast-proto/gen/go/agentanycast/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// relayBinaryPath returns the path to the relay binary.
// Build with: go build -o bin/relay ./cmd/relay
func relayBinaryPath() string {
	if p := os.Getenv("RELAY_BINARY"); p != "" {
		return p
	}
	// Check common locations.
	for _, p := range []string{
		"../../bin/relay",
		"../../agentanycast-relay",
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// getFreePort asks the OS for a free TCP port.
func getFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("get free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// startRelay starts a relay process and returns a cleanup function.
func startRelay(t *testing.T) (registryAddr string, cleanup func()) {
	t.Helper()

	binary := relayBinaryPath()
	if binary == "" {
		t.Skip("relay binary not found; build with: go build -o bin/relay ./cmd/relay")
	}

	p2pPort := getFreePort(t)
	grpcPort := getFreePort(t)
	registryAddr = fmt.Sprintf("127.0.0.1:%d", grpcPort)

	cmd := exec.Command(binary,
		"--listen", fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", p2pPort),
		"--registry-listen", registryAddr,
		"--registry-ttl", "5s",
		"--log-level", "error",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("start relay: %v", err)
	}

	// Wait for gRPC port to be ready.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", registryAddr, 200*time.Millisecond)
		if err == nil {
			conn.Close()
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	return registryAddr, func() {
		cmd.Process.Signal(os.Interrupt)
		cmd.Wait()
	}
}

func TestRegistryIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	addr, cleanup := startRelay(t)
	defer cleanup()

	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("connect to registry: %v", err)
	}
	defer conn.Close()

	client := pb.NewRegistryServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. Register skills for peer-A.
	_, err = client.RegisterSkills(ctx, &pb.RegisterSkillsRequest{
		PeerId: "12D3KooWTestPeerA",
		Skills: []*pb.SkillInfo{
			{SkillId: "summarize", Description: "Summarize text"},
			{SkillId: "translate", Description: "Translate text", Tags: map[string]string{"lang": "en"}},
		},
		AgentName:        "Agent A",
		AgentDescription: "Integration test agent",
	})
	if err != nil {
		t.Fatalf("RegisterSkills: %v", err)
	}

	// 2. Discover by skill.
	resp, err := client.DiscoverBySkill(ctx, &pb.DiscoverBySkillRequest{
		SkillId: "summarize",
	})
	if err != nil {
		t.Fatalf("DiscoverBySkill: %v", err)
	}
	if len(resp.Agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(resp.Agents))
	}
	if resp.Agents[0].PeerId != "12D3KooWTestPeerA" {
		t.Fatalf("expected peer-A, got %s", resp.Agents[0].PeerId)
	}
	if resp.Agents[0].AgentName != "Agent A" {
		t.Fatalf("expected Agent A, got %s", resp.Agents[0].AgentName)
	}

	// 3. Register a second peer with the same skill.
	_, err = client.RegisterSkills(ctx, &pb.RegisterSkillsRequest{
		PeerId: "12D3KooWTestPeerB",
		Skills: []*pb.SkillInfo{
			{SkillId: "summarize", Description: "Also summarizes"},
		},
		AgentName: "Agent B",
	})
	if err != nil {
		t.Fatalf("RegisterSkills peer-B: %v", err)
	}

	// 4. Discover should now return both.
	resp, err = client.DiscoverBySkill(ctx, &pb.DiscoverBySkillRequest{
		SkillId: "summarize",
	})
	if err != nil {
		t.Fatalf("DiscoverBySkill after second registration: %v", err)
	}
	if len(resp.Agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(resp.Agents))
	}

	// 5. Discover with tag filter.
	resp, err = client.DiscoverBySkill(ctx, &pb.DiscoverBySkillRequest{
		SkillId: "translate",
		Tags:    map[string]string{"lang": "en"},
	})
	if err != nil {
		t.Fatalf("DiscoverBySkill with tags: %v", err)
	}
	if len(resp.Agents) != 1 {
		t.Fatalf("expected 1 agent with tag filter, got %d", len(resp.Agents))
	}

	// 6. Heartbeat.
	hbResp, err := client.Heartbeat(ctx, &pb.HeartbeatRequest{
		PeerId: "12D3KooWTestPeerA",
	})
	if err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	if hbResp.ExpiresAt == nil {
		t.Fatal("expected non-nil ExpiresAt")
	}

	// 7. Unregister one skill.
	_, err = client.UnregisterSkills(ctx, &pb.UnregisterSkillsRequest{
		PeerId:   "12D3KooWTestPeerA",
		SkillIds: []string{"summarize"},
	})
	if err != nil {
		t.Fatalf("UnregisterSkills: %v", err)
	}

	// 8. Verify skill was removed.
	resp, err = client.DiscoverBySkill(ctx, &pb.DiscoverBySkillRequest{
		SkillId: "summarize",
	})
	if err != nil {
		t.Fatalf("DiscoverBySkill after unregister: %v", err)
	}
	// Only peer-B should remain.
	if len(resp.Agents) != 1 {
		t.Fatalf("expected 1 agent after unregister, got %d", len(resp.Agents))
	}
	if resp.Agents[0].PeerId != "12D3KooWTestPeerB" {
		t.Fatalf("expected peer-B, got %s", resp.Agents[0].PeerId)
	}

	// 9. Translate skill should still be there for peer-A.
	resp, err = client.DiscoverBySkill(ctx, &pb.DiscoverBySkillRequest{
		SkillId: "translate",
	})
	if err != nil {
		t.Fatalf("DiscoverBySkill translate: %v", err)
	}
	if len(resp.Agents) != 1 {
		t.Fatalf("expected 1 agent for translate, got %d", len(resp.Agents))
	}

	// 10. Test TTL expiration — wait for registry-ttl (5s) + cleanup.
	t.Log("waiting for TTL expiration...")
	time.Sleep(7 * time.Second)

	resp, err = client.DiscoverBySkill(ctx, &pb.DiscoverBySkillRequest{
		SkillId: "translate",
	})
	if err != nil {
		t.Fatalf("DiscoverBySkill after TTL: %v", err)
	}
	if len(resp.Agents) != 0 {
		// Log what we got for debugging.
		var peerIDs []string
		for _, a := range resp.Agents {
			peerIDs = append(peerIDs, a.PeerId)
		}
		t.Fatalf("expected 0 agents after TTL, got %d: %s", len(resp.Agents), strings.Join(peerIDs, ", "))
	}
}
