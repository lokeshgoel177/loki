//go:build !windows

package ipc

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
)

// UnixTransport implements the Transport interface using POSIX Unix Domain Sockets.
type UnixTransport struct{}

// NewDefaultTransport returns the platform-appropriate Transport implementation for POSIX.
func NewDefaultTransport() Transport {
	return &UnixTransport{}
}

// GetDefaultAddress computes the default Unix Domain Socket filesystem path.
// It checks $XDG_RUNTIME_DIR/loki/agentd.sock first, falling back to ~/.loki/agentd.sock.
func GetDefaultAddress() (string, error) {
	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		return filepath.Join(runtimeDir, "loki", "agentd.sock"), nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("ipc: unable to resolve user home directory: %w", err)
	}
	return filepath.Join(homeDir, ".loki", "agentd.sock"), nil
}

// Listen creates a Unix domain socket listener at the specified filesystem path,
// ensuring parent directories exist and enforcing owner-only (0600) file permissions.
func (t *UnixTransport) Listen(addr string) (net.Listener, error) {
	dir := filepath.Dir(addr)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("ipc: failed to create socket directory: %w", err)
	}

	// Remove stale unclosed socket file if present
	if fi, err := os.Stat(addr); err == nil {
		if fi.Mode().Type() == os.ModeSocket {
			_ = os.Remove(addr)
		}
	}

	listener, err := net.Listen("unix", addr)
	if err != nil {
		return nil, fmt.Errorf("ipc: listen unix failed: %w", err)
	}

	if err := os.Chmod(addr, 0600); err != nil {
		listener.Close()
		return nil, fmt.Errorf("ipc: failed to set permissions on socket: %w", err)
	}

	return listener, nil
}

// Dial connects to an existing Unix domain socket server at the specified filesystem path.
func (t *UnixTransport) Dial(addr string) (net.Conn, error) {
	conn, err := net.Dial("unix", addr)
	if err != nil {
		return nil, fmt.Errorf("ipc: dial unix failed: %w", err)
	}
	return conn, nil
}
