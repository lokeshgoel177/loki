package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/lokeshgoel177/loki/internal/config"
	"github.com/lokeshgoel177/loki/internal/ipc"
)

// DefaultSpawnTimeout defines the maximum duration to wait for a spawned daemon to become live (5s).
const DefaultSpawnTimeout = 5 * time.Second

// Supervisor manages client-side liveness detection and race-free background auto-spawning.
type Supervisor struct {
	cfg       *config.Config
	transport ipc.Transport
	daemonCmd []string      // Custom command override (used in integration testing)
	timeout   time.Duration // Deadline for daemon socket readiness
}

// NewSupervisor constructs a Supervisor for the given configuration.
func NewSupervisor(cfg *config.Config, transport ipc.Transport) *Supervisor {
	if transport == nil {
		transport = ipc.NewDefaultTransport()
	}
	return &Supervisor{
		cfg:       cfg,
		transport: transport,
		timeout:   DefaultSpawnTimeout,
	}
}

// SetCommand overrides the command line executed when auto-spawning (primarily for tests).
func (s *Supervisor) SetCommand(cmd []string) {
	s.daemonCmd = cmd
}

// SetTimeout configures the maximum deadline for auto-spawn readiness.
func (s *Supervisor) SetTimeout(d time.Duration) {
	if d > 0 {
		s.timeout = d
	}
}

// IsLive probes the IPC socket/pipe. Returns true if agentd is currently accepting connections.
func (s *Supervisor) IsLive() bool {
	conn, err := s.transport.Dial(s.cfg.SocketPath)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// EnsureRunning checks if agentd is alive; if dead or missing, safely launches a background daemon.
func (s *Supervisor) EnsureRunning() error {
	// Fast path: daemon is already live
	if s.IsLive() {
		return nil
	}

	// Slow path: acquire exclusive spawn lock to prevent duplicate concurrent forks
	return s.spawnDetached()
}

// spawnDetached acquires spawn.lock, forks agentd in a detached process, and waits for socket readiness.
func (s *Supervisor) spawnDetached() error {
	lockDir := filepath.Dir(s.cfg.SpawnLockFile())
	if err := os.MkdirAll(lockDir, 0o700); err != nil {
		return fmt.Errorf("daemon: failed to create lock directory %q: %w", lockDir, err)
	}

	lockFile, err := os.OpenFile(s.cfg.SpawnLockFile(), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("daemon: failed to open spawn lockfile: %w", err)
	}
	defer lockFile.Close()

	if err := lockFileExclusive(lockFile); err != nil {
		return fmt.Errorf("daemon: failed to acquire spawn lock: %w", err)
	}
	defer unlockFile(lockFile)

	// Re-check liveness inside lock: another client may have spawned the daemon while we waited for lock
	if s.IsLive() {
		return nil
	}

	cmd, err := s.buildSpawnCommand()
	if err != nil {
		return err
	}

	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	configureDetachedProcess(cmd)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("daemon: failed to start detached process: %w", err)
	}

	// Poll IsLive() with deadline before releasing spawn.lock.
	// This eliminates the race window where another client grabs the lock before the socket is bound.
	deadline := time.Now().Add(s.timeout)
	pollInterval := 50 * time.Millisecond

	for time.Now().Before(deadline) {
		if s.IsLive() {
			// Daemon is up, socket/pipe is bound and accepting connections
			_ = cmd.Process.Release()
			return nil
		}
		time.Sleep(pollInterval)
	}

	// Timeout exceeded: terminate orphan process
	_ = cmd.Process.Kill()
	return fmt.Errorf("daemon: spawned process failed to become live within %v deadline", s.timeout)
}

// buildSpawnCommand locates the target agentd binary or uses the configured custom command.
func (s *Supervisor) buildSpawnCommand() (*exec.Cmd, error) {
	if len(s.daemonCmd) > 0 {
		return exec.Command(s.daemonCmd[0], s.daemonCmd[1:]...), nil
	}

	execPath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("daemon: failed to resolve executable path: %w", err)
	}

	dir := filepath.Dir(execPath)
	agentdBin := filepath.Join(dir, "agentd")
	if runtime.GOOS == "windows" {
		agentdBin += ".exe"
	}

	if _, err := os.Stat(agentdBin); err == nil {
		return exec.Command(agentdBin), nil
	}

	// Fallback to launching current executable with "daemon" command
	return exec.Command(execPath, "daemon"), nil
}
