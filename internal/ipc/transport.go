package ipc

import "net"

// Transport defines the common abstraction for creating local IPC servers
// and connecting clients, hiding OS-specific socket or named pipe mechanics.
type Transport interface {
	// Listen creates a network listener bound to the given IPC address or path.
	Listen(addr string) (net.Listener, error)

	// Dial establishes a connection to a listening IPC server at the given address.
	Dial(addr string) (net.Conn, error)
}
