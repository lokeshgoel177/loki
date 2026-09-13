//go:build !windows

package daemon

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// lockFileExclusive acquires an exclusive advisory lock using POSIX flock.
func lockFileExclusive(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
}

// unlockFile releases the advisory lock using POSIX flock.
func unlockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}

// isProcessAlive checks if a process with the given PID exists using signal 0.
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}

	// Sending signal 0 performs error checking without actually delivering a signal.
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}

	// EPERM means the process exists but is owned by another user (still alive).
	if errors.Is(err, syscall.EPERM) {
		return true
	}

	// ESRCH means no such process exists.
	return false
}

// configureDetachedProcess configures the child process to run in its own process group.
func configureDetachedProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
}
