package daemon_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lokeshgoel177/loki/internal/daemon"
)

func TestPIDLifecycle(t *testing.T) {
	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "agentd.pid")

	// Read non-existent file should error
	_, err := daemon.ReadPID(pidFile)
	if err == nil {
		t.Errorf("expected error reading non-existent PID file, got nil")
	}

	// Write PID
	currentPID := os.Getpid()
	if err := daemon.WritePID(pidFile, currentPID); err != nil {
		t.Fatalf("WritePID failed: %v", err)
	}

	// Read written PID
	readPID, err := daemon.ReadPID(pidFile)
	if err != nil {
		t.Fatalf("ReadPID failed: %v", err)
	}
	if readPID != currentPID {
		t.Errorf("ReadPID = %d; want %d", readPID, currentPID)
	}

	// Remove PID
	if err := daemon.RemovePID(pidFile); err != nil {
		t.Fatalf("RemovePID failed: %v", err)
	}

	// Verify file is gone
	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Errorf("PID file was not removed")
	}

	// Removing again should not error
	if err := daemon.RemovePID(pidFile); err != nil {
		t.Errorf("RemovePID on non-existent file returned error: %v", err)
	}
}

func TestCheckStalePID(t *testing.T) {
	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "test.pid")

	// 1. Non-existent PID file
	pid, isLive, err := daemon.CheckStalePID(pidFile)
	if err != nil || isLive || pid != 0 {
		t.Errorf("non-existent file: got (%d, %v, %v); want (0, false, nil)", pid, isLive, err)
	}

	// 2. Active PID (our current test process)
	currentPID := os.Getpid()
	if err := daemon.WritePID(pidFile, currentPID); err != nil {
		t.Fatalf("WritePID failed: %v", err)
	}

	pid, isLive, err = daemon.CheckStalePID(pidFile)
	if err != nil || !isLive || pid != currentPID {
		t.Errorf("live process: got (%d, %v, %v); want (%d, true, nil)", pid, isLive, err, currentPID)
	}

	// 3. Stale / Dead PID (using an impossibly high PID like 99999999)
	deadPID := 99999999
	if err := daemon.WritePID(pidFile, deadPID); err != nil {
		t.Fatalf("WritePID failed: %v", err)
	}

	pid, isLive, err = daemon.CheckStalePID(pidFile)
	if err != nil || isLive || pid != deadPID {
		t.Errorf("stale process: got (%d, %v, %v); want (%d, false, nil)", pid, isLive, err, deadPID)
	}

	// Stale file should have been automatically unlinked
	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Errorf("stale PID file was not cleaned up automatically")
	}

	// 4. Corrupted PID file content
	if err := os.WriteFile(pidFile, []byte("not-a-number"), 0o600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	pid, isLive, err = daemon.CheckStalePID(pidFile)
	if err != nil || isLive || pid != 0 {
		t.Errorf("corrupted file: got (%d, %v, %v); want (0, false, nil)", pid, isLive, err)
	}
}
