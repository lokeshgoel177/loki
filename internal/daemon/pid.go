package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ReadPID reads and parses the PID from the specified file.
func ReadPID(pidPath string) (int, error) {
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return 0, err
	}
	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return 0, fmt.Errorf("daemon: invalid PID string %q in %s: %w", pidStr, pidPath, err)
	}
	return pid, nil
}

// WritePID writes the current PID to the specified file, ensuring parent directory exists.
func WritePID(pidPath string, pid int) error {
	dir := filepath.Dir(pidPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("daemon: failed to create PID directory %q: %w", dir, err)
	}
	return os.WriteFile(pidPath, []byte(strconv.Itoa(pid)+"\n"), 0o600)
}

// RemovePID safely deletes the PID file. Returns nil if the file does not exist.
func RemovePID(pidPath string) error {
	err := os.Remove(pidPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("daemon: failed to remove PID file %q: %w", pidPath, err)
	}
	return nil
}

// CheckStalePID inspects an existing PID file:
// - If the file does not exist, returns (0, false, nil).
// - If the process is alive, returns (pid, true, nil).
// - If the process is dead (stale), removes the stale file and returns (pid, false, nil).
func CheckStalePID(pidPath string) (int, bool, error) {
	pid, err := ReadPID(pidPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, false, nil
		}
		// Corrupted PID file: clean it up
		_ = os.Remove(pidPath)
		return 0, false, nil
	}

	if isProcessAlive(pid) {
		return pid, true, nil
	}

	// Process is dead: remove stale PID file
	_ = os.Remove(pidPath)
	return pid, false, nil
}
