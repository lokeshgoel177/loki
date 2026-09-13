package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync"

	"github.com/lokeshgoel177/loki/internal/config"
	"github.com/lokeshgoel177/loki/internal/events"
	"github.com/lokeshgoel177/loki/internal/ipc"
	"github.com/lokeshgoel177/loki/internal/protocol"
)

// Server encapsulates the background agentd daemon runtime.
type Server struct {
	cfg       *config.Config
	transport ipc.Transport
	broker    *events.EventBroker
	listener  net.Listener

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu      sync.Mutex
	running bool
	conns   map[net.Conn]struct{}
}

// NewServer constructs an unstarted daemon Server.
func NewServer(cfg *config.Config, broker *events.EventBroker, transport ipc.Transport) *Server {
	if transport == nil {
		transport = ipc.NewDefaultTransport()
	}
	if broker == nil {
		broker = events.NewEventBroker()
	}

	ctx, cancel := context.WithCancel(context.Background())
	return &Server{
		cfg:       cfg,
		transport: transport,
		broker:    broker,
		ctx:       ctx,
		cancel:    cancel,
		conns:     make(map[net.Conn]struct{}),
	}
}

// Start validates PID locks, binds the IPC listener, and begins accepting connections in the background.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("daemon: server is already running")
	}

	// 1. Check for stale or active daemon PID
	existingPID, isLive, err := CheckStalePID(s.cfg.PIDFile())
	if err != nil {
		return fmt.Errorf("daemon: failed to check PID file: %w", err)
	}
	if isLive {
		return fmt.Errorf("daemon: agentd is already running with PID %d", existingPID)
	}

	// 2. Bind the local IPC listener (Unix socket or Windows named pipe)
	listener, err := s.transport.Listen(s.cfg.SocketPath)
	if err != nil {
		return fmt.Errorf("daemon: failed to bind IPC listener on %q: %w", s.cfg.SocketPath, err)
	}
	s.listener = listener

	// 3. Write our current PID
	if err := WritePID(s.cfg.PIDFile(), os.Getpid()); err != nil {
		_ = listener.Close()
		return fmt.Errorf("daemon: failed to write PID file: %w", err)
	}

	s.running = true
	slog.Info("agentd started", "pid", os.Getpid(), "address", s.cfg.SocketPath)

	// 4. Start accept loop
	s.wg.Add(1)
	go s.acceptLoop()

	return nil
}

// Stop initiates a graceful shutdown of the daemon, closing connections and cleaning up the PID file.
func (s *Server) Stop() error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false

	// Cancel context to notify all workers
	s.cancel()

	// Close listener to unblock acceptLoop()
	var closeErr error
	if s.listener != nil {
		closeErr = s.listener.Close()
	}

	// Close all active client connections
	for conn := range s.conns {
		_ = conn.Close()
	}
	s.mu.Unlock()

	// Wait for background connection handlers and accept loop to exit
	s.wg.Wait()

	// Remove PID file
	if err := RemovePID(s.cfg.PIDFile()); err != nil {
		slog.Warn("failed to remove PID file during shutdown", "error", err)
	}

	slog.Info("agentd cleanly stopped")
	return closeErr
}

// acceptLoop accepts incoming client connections until the server is stopped.
func (s *Server) acceptLoop() {
	defer s.wg.Done()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			// Check if listener was closed due to server shutdown
			select {
			case <-s.ctx.Done():
				return
			default:
				slog.Debug("accept error", "error", err)
				return
			}
		}

		s.mu.Lock()
		if !s.running {
			s.mu.Unlock()
			_ = conn.Close()
			return
		}

		// Enforce MaxConnections limit
		if s.cfg.MaxConnections > 0 && len(s.conns) >= s.cfg.MaxConnections {
			s.mu.Unlock()
			slog.Warn("rejecting connection: max connections reached", "limit", s.cfg.MaxConnections)
			_ = conn.Close()
			continue
		}

		s.conns[conn] = struct{}{}
		s.mu.Unlock()

		s.wg.Add(1)
		go s.handleConnection(conn)
	}
}

// handleConnection manages the lifecycle of an individual client connection.
func (s *Server) handleConnection(conn net.Conn) {
	defer func() {
		s.mu.Lock()
		delete(s.conns, conn)
		s.mu.Unlock()
		_ = conn.Close()
		s.wg.Done()
	}()

	fc := protocol.NewFramedConn(conn)

	for {
		env, err := fc.ReadEnvelope()
		if err != nil {
			// Client disconnected or closed connection
			return
		}

		// Basic handshake/ping handler; future milestones will route to the ReAct session loop
		if env.Method == "ping" {
			resp := &protocol.MessageEnvelope{
				JSONRPC:   protocol.JSONRPCVersion,
				ID:        env.ID,
				Result:    []byte(`"pong"`),
				Timestamp: env.Timestamp,
			}
			if err := fc.WriteEnvelope(resp); err != nil {
				return
			}
		}
	}
}

// IsRunning reports whether the server is actively running.
func (s *Server) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// EventBroker returns the internal pub/sub broker.
func (s *Server) EventBroker() *events.EventBroker {
	return s.broker
}
