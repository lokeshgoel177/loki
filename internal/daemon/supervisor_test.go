package daemon_test

import (
	"testing"
	"time"

	"github.com/lokeshgoel177/loki/internal/daemon"
	"github.com/lokeshgoel177/loki/internal/ipc"
)

func TestSupervisor_IsLive(t *testing.T) {
	cfg := testConfig(t)
	transport := ipc.NewDefaultTransport()
	supervisor := daemon.NewSupervisor(cfg, transport)

	// 1. When server is not running, IsLive must return false
	if supervisor.IsLive() {
		t.Errorf("expected IsLive() == false before server starts")
	}

	// 2. Start server
	server := daemon.NewServer(cfg, nil, transport)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	// 3. When server is running, IsLive must return true
	if !supervisor.IsLive() {
		t.Errorf("expected IsLive() == true while server is active")
	}

	// 4. Stop server
	if err := server.Stop(); err != nil {
		t.Fatalf("failed to stop server: %v", err)
	}

	// 5. Must return false after stop
	if supervisor.IsLive() {
		t.Errorf("expected IsLive() == false after server stops")
	}
}

func TestSupervisor_EnsureRunning_FastPath(t *testing.T) {
	cfg := testConfig(t)
	transport := ipc.NewDefaultTransport()

	server := daemon.NewServer(cfg, nil, transport)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	supervisor := daemon.NewSupervisor(cfg, transport)

	// Fast path should return immediately with no error since server is already running
	if err := supervisor.EnsureRunning(); err != nil {
		t.Errorf("EnsureRunning fast path failed: %v", err)
	}
}

func TestSupervisor_SpawnDetached_Timeout(t *testing.T) {
	cfg := testConfig(t)
	transport := ipc.NewDefaultTransport()
	supervisor := daemon.NewSupervisor(cfg, transport)

	// Configure a command that does not bind the socket (e.g. "go version")
	supervisor.SetCommand([]string{"go", "version"})
	supervisor.SetTimeout(150 * time.Millisecond)

	err := supervisor.EnsureRunning()
	if err == nil {
		t.Fatalf("expected EnsureRunning to timeout and return error, got nil")
	}
}
