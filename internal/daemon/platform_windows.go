//go:build windows

package daemon

import (
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// lockFileExclusive acquires a mandatory exclusive byte-range lock on the file.
func lockFileExclusive(f *os.File) error {
	var overlapped windows.Overlapped
	// Lock byte 0 to 1 with exclusive lock flag (blocks until acquired)
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &overlapped)
}

// unlockFile releases the exclusive byte-range lock on the file.
func unlockFile(f *os.File) error {
	var overlapped windows.Overlapped
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &overlapped)
}

// isProcessAlive checks if a process with the given PID is actively executing on Windows.
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}

	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)

	var exitCode uint32
	if err := windows.GetExitCodeProcess(handle, &exitCode); err != nil {
		return false
	}

	// 259 (0x103) is the Win32 STILL_ACTIVE status code
	const statusStillActive = 259
	return exitCode == statusStillActive
}

// configureDetachedProcess configures the child process to run detached in a new process group.
func configureDetachedProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS,
	}
}
