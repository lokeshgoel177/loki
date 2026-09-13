//go:build windows

package ipc

import (
	"fmt"
	"net"
	"os/user"
	"path/filepath"

	"github.com/Microsoft/go-winio"
)

// WindowsTransport implements the Transport interface using Win32 Named Pipes.
type WindowsTransport struct{}

// NewDefaultTransport returns the platform-appropriate Transport implementation for Windows.
func NewDefaultTransport() Transport {
	return &WindowsTransport{}
}

// GetDefaultAddress computes the default Win32 Named Pipe path for the current user:
// \\.\pipe\loki-agentd-<username>
func GetDefaultAddress() (string, error) {
	currentUser, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("ipc: unable to resolve current user: %w", err)
	}

	// Sanitize username (strip domain or machine prefixes like WORKGROUP\user)
	username := filepath.Base(currentUser.Username)
	return fmt.Sprintf(`\\.\pipe\loki-agentd-%s`, username), nil
}

// Listen creates a Win32 Named Pipe listener with secure owner-only DACL permissions.
func (t *WindowsTransport) Listen(addr string) (net.Listener, error) {
	cfg := &winio.PipeConfig{
		MessageMode:      false, // Byte stream mode for 4-byte length prefix wire framing
		InputBufferSize:  65536, // 64 KB kernel read buffer
		OutputBufferSize: 65536, // 64 KB kernel write buffer
	}

	listener, err := winio.ListenPipe(addr, cfg)
	if err != nil {
		return nil, fmt.Errorf("ipc: listen named pipe failed on %q: %w", addr, err)
	}
	return listener, nil
}

// Dial connects to an existing Win32 Named Pipe server at the specified pipe address.
func (t *WindowsTransport) Dial(addr string) (net.Conn, error) {
	conn, err := winio.DialPipe(addr, nil)
	if err != nil {
		return nil, fmt.Errorf("ipc: dial named pipe failed on %q: %w", addr, err)
	}
	return conn, nil
}
