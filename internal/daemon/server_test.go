package daemon_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/lokeshgoel177/loki/internal/config"
	"github.com/lokeshgoel177/loki/internal/daemon"
	"github.com/lokeshgoel177/loki/internal/ipc"
	"github.com/lokeshgoel177/loki/internal/protocol"
)

func testAddress(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		return fmt.Sprintf(`\\.\pipe\loki-test-%d-%d`, os.Getpid(), time.Now().UnixNano())
	}
	return filepath.Join(t.TempDir(), "test.sock")
}

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	tempDir := t.TempDir()
	return &config.Config{
		ConfigDir:      tempDir,
		LokiDir:        tempDir,
		SocketPath:     testAddress(t),
		IdleTimeout:    config.Duration(30 * time.Minute),
		MaxConnections: 10,
		LogLevel:       "debug",
	}
}

func TestServer_StartAndStop(t *testing.T) {
	cfg := testConfig(t)
	transport := ipc.NewDefaultTransport()
	server := daemon.NewServer(cfg, nil, transport)

	if server.IsRunning() {
		t.Fatalf("expected server not to be running before Start()")
	}

	// 1. Start Server
	if err := server.Start(); err != nil {
		t.Fatalf("Server.Start failed: %v", err)
	}

	if !server.IsRunning() {
		t.Fatalf("expected server to be running after Start()")
	}

	// Verify PID file exists and contains current PID
	pid, err := daemon.ReadPID(cfg.PIDFile())
	if err != nil {
		t.Fatalf("failed to read PID file: %v", err)
	}
	if pid != os.Getpid() {
		t.Errorf("PID = %d; want %d", pid, os.Getpid())
	}

	// 2. Client ping/pong communication over IPC
	conn, err := transport.Dial(cfg.SocketPath)
	if err != nil {
		t.Fatalf("client failed to dial server: %v", err)
	}
	defer conn.Close()

	fc := protocol.NewFramedConn(conn)
	req := &protocol.MessageEnvelope{
		JSONRPC:   protocol.JSONRPCVersion,
		ID:        "test-ping-1",
		Method:    "ping",
		Timestamp: time.Now().UTC(),
	}
	if err := fc.WriteEnvelope(req); err != nil {
		t.Fatalf("failed to write ping request: %v", err)
	}

	resp, err := fc.ReadEnvelope()
	if err != nil {
		t.Fatalf("failed to read ping response: %v", err)
	}
	if string(resp.Result) != `"pong"` {
		t.Errorf("unexpected ping result: got %s, want %q", string(resp.Result), "pong")
	}

	// 3. Stop Server
	if err := server.Stop(); err != nil {
		t.Fatalf("Server.Stop failed: %v", err)
	}

	if server.IsRunning() {
		t.Fatalf("expected server not to be running after Stop()")
	}

	// Verify PID file was removed
	if _, err := os.Stat(cfg.PIDFile()); !os.IsNotExist(err) {
		t.Errorf("PID file was not removed after Stop()")
	}

	// Calling Stop() again should be a safe no-op
	if err := server.Stop(); err != nil {
		t.Errorf("second Stop() returned error: %v", err)
	}
}

func TestServer_DuplicateStartFails(t *testing.T) {
	cfg := testConfig(t)
	transport := ipc.NewDefaultTransport()

	server1 := daemon.NewServer(cfg, nil, transport)
	if err := server1.Start(); err != nil {
		t.Fatalf("server1 failed to start: %v", err)
	}
	defer server1.Stop()

	// Calling Start on already running server instance should fail
	if err := server1.Start(); err == nil {
		t.Errorf("expected error on duplicate Start() on same instance, got nil")
	}

	// Creating second server pointing to same PID file and socket should fail
	server2 := daemon.NewServer(cfg, nil, transport)
	err := server2.Start()
	if err == nil {
		server2.Stop()
		t.Fatalf("expected server2 to fail starting when server1 is active, got nil")
	}
}

func TestServer_MaxConnections(t *testing.T) {
	cfg := testConfig(t)
	cfg.MaxConnections = 2
	transport := ipc.NewDefaultTransport()

	server := daemon.NewServer(cfg, nil, transport)
	if err := server.Start(); err != nil {
		t.Fatalf("server failed to start: %v", err)
	}
	defer server.Stop()

	// Connect first client
	c1, err := transport.Dial(cfg.SocketPath)
	if err != nil {
		t.Fatalf("c1 failed to dial: %v", err)
	}
	defer c1.Close()

	// Connect second client
	c2, err := transport.Dial(cfg.SocketPath)
	if err != nil {
		t.Fatalf("c2 failed to dial: %v", err)
	}
	defer c2.Close()

	// Third client should dial, but server will reject/close connection
	c3, err := transport.Dial(cfg.SocketPath)
	if err == nil {
		fc3 := protocol.NewFramedConn(c3)
		// Should read EOF/error because server closes rejected connection
		_, readErr := fc3.ReadEnvelope()
		if readErr == nil {
			t.Errorf("expected read error on rejected third connection, got nil")
		}
		c3.Close()
	}
}
