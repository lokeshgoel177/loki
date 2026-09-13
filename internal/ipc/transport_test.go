package ipc_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lokeshgoel177/loki/internal/ipc"
	"github.com/lokeshgoel177/loki/internal/protocol"
)

// testAddress generates a unique, platform-appropriate IPC address for test isolation.
// On Windows, named pipes must adhere to the \\.\pipe\<name> syntax.
// On Unix, domain sockets use a filesystem path (t.TempDir ensures automatic cleanup).
func testAddress(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		return fmt.Sprintf(`\\.\pipe\loki-test-%d-%d`, os.Getpid(), time.Now().UnixNano())
	}
	return filepath.Join(t.TempDir(), "test.sock")
}

// TestGetDefaultAddress verifies that GetDefaultAddress returns a well-formed,
// platform-compliant default path for the daemon on both Windows and Unix systems.
func TestGetDefaultAddress(t *testing.T) {
	addr, err := ipc.GetDefaultAddress()
	if err != nil {
		t.Fatalf("unexpected error resolving default address: %v", err)
	}
	if addr == "" {
		t.Fatal("expected non-empty default address")
	}

	if runtime.GOOS == "windows" {
		expectedPrefix := `\\.\pipe\loki-agentd-`
		if len(addr) <= len(expectedPrefix) || addr[:len(expectedPrefix)] != expectedPrefix {
			t.Fatalf("expected Windows pipe address to start with %q, got %q", expectedPrefix, addr)
		}
	} else {
		if filepath.Ext(addr) != ".sock" {
			t.Fatalf("expected Unix socket address to end with .sock, got %q", addr)
		}
	}
}

// TestTransport_DialNonExistent verifies that attempting to dial an address with
// no active listener fails promptly with an error.
func TestTransport_DialNonExistent(t *testing.T) {
	transport := ipc.NewDefaultTransport()
	addr := testAddress(t)

	conn, err := transport.Dial(addr)
	if err == nil {
		_ = conn.Close()
		t.Fatalf("expected dial error on non-existent address %q, got nil", addr)
	}
}

// TestTransport_ListenClose verifies that a listener binds cleanly to an address
// and unbinds cleanly when Closed.
func TestTransport_ListenClose(t *testing.T) {
	transport := ipc.NewDefaultTransport()
	addr := testAddress(t)

	listener, err := transport.Listen(addr)
	if err != nil {
		t.Fatalf("failed to listen on %q: %v", addr, err)
	}

	if err := listener.Close(); err != nil {
		t.Fatalf("failed to close listener: %v", err)
	}
}

// TestTransport_BasicRawEcho verifies raw byte transmission over the transport
// without framing, confirming fundamental OS socket/pipe mechanics.
func TestTransport_BasicRawEcho(t *testing.T) {
	transport := ipc.NewDefaultTransport()
	addr := testAddress(t)

	listener, err := transport.Listen(addr)
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	payload := []byte("ping from loki raw client")
	echoDone := make(chan struct{})

	// Server goroutine: accepts 1 connection, echoes back exact bytes, then exits
	go func() {
		defer close(echoDone)
		conn, err := listener.Accept()
		if err != nil {
			t.Errorf("server accept failed: %v", err)
			return
		}
		defer conn.Close()

		buf := make([]byte, len(payload))
		if _, err := io.ReadFull(conn, buf); err != nil {
			t.Errorf("server read failed: %v", err)
			return
		}

		if _, err := conn.Write(buf); err != nil {
			t.Errorf("server echo write failed: %v", err)
			return
		}
	}()

	clientConn, err := transport.Dial(addr)
	if err != nil {
		t.Fatalf("client dial failed: %v", err)
	}
	defer clientConn.Close()

	if _, err := clientConn.Write(payload); err != nil {
		t.Fatalf("client write failed: %v", err)
	}

	reply := make([]byte, len(payload))
	if _, err := io.ReadFull(clientConn, reply); err != nil {
		t.Fatalf("client read echo failed: %v", err)
	}

	if !bytes.Equal(payload, reply) {
		t.Fatalf("expected payload %q, got %q", string(payload), string(reply))
	}

	<-echoDone
}

// TestTransport_BidirectionalStreaming_10kMessages satisfies Milestone 0.2 exit criterion:
// Stress-tests bidirectional streaming over real OS IPC by exchanging 10,000 length-prefixed
// JSON-RPC envelopes across concurrent writer goroutines.
func TestTransport_BidirectionalStreaming_10kMessages(t *testing.T) {
	transport := ipc.NewDefaultTransport()
	addr := testAddress(t)

	listener, err := transport.Listen(addr)
	if err != nil {
		t.Fatalf("failed to listen on %q: %v", addr, err)
	}
	defer listener.Close()

	const totalMessages = 10_000
	const numWorkers = 10
	const msgsPerWorker = totalMessages / numWorkers

	serverDone := make(chan struct{})
	var serverErr error

	// Server goroutine: accepts client connection and handles request/response streaming loop
	go func() {
		defer close(serverDone)
		rawConn, err := listener.Accept()
		if err != nil {
			serverErr = fmt.Errorf("accept error: %w", err)
			return
		}
		defer rawConn.Close()

		serverFramed := protocol.NewFramedConn(rawConn)
		defer serverFramed.Close()

		for {
			env, err := serverFramed.ReadEnvelope()
			if err != nil {
				if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
					return
				}
				// Client closing connection may surface as an OS pipe/socket broken error
				return
			}

			// Respond with echo result matching ID and Sequence ID
			resp := &protocol.MessageEnvelope{
				JSONRPC:   protocol.JSONRPCVersion,
				ID:        env.ID,
				SeqID:     env.SeqID,
				Result:    env.Params,
				Timestamp: time.Now().UTC(),
			}

			if err := serverFramed.WriteEnvelope(resp); err != nil {
				serverErr = fmt.Errorf("server write failed: %w", err)
				return
			}
		}
	}()

	// Connect client to server
	clientRawConn, err := transport.Dial(addr)
	if err != nil {
		t.Fatalf("client dial failed on %q: %v", addr, err)
	}
	clientFramed := protocol.NewFramedConn(clientRawConn)
	defer clientFramed.Close()

	var receivedCount atomic.Int64
	clientReadDone := make(chan struct{})
	var clientReadErr error

	// Dedicated reader goroutine on client: reads incoming response envelopes
	go func() {
		defer close(clientReadDone)
		for {
			resp, err := clientFramed.ReadEnvelope()
			if err != nil {
				if receivedCount.Load() == int64(totalMessages) {
					return
				}
				clientReadErr = fmt.Errorf("client read error at count %d: %w", receivedCount.Load(), err)
				return
			}

			if resp.JSONRPC != protocol.JSONRPCVersion {
				clientReadErr = fmt.Errorf("invalid jsonrpc version: %s", resp.JSONRPC)
				return
			}

			if resp.ID == "" {
				clientReadErr = fmt.Errorf("missing response ID")
				return
			}

			count := receivedCount.Add(1)
			if count == int64(totalMessages) {
				return
			}
		}
	}()

	// Launch concurrent writer worker goroutines
	startTime := time.Now()
	var writeWg sync.WaitGroup
	writeWg.Add(numWorkers)

	for w := 0; w < numWorkers; w++ {
		go func(workerID int) {
			defer writeWg.Done()
			for m := 0; m < msgsPerWorker; m++ {
				seq := uint64(workerID*msgsPerWorker + m + 1)
				env := &protocol.MessageEnvelope{
					JSONRPC:   protocol.JSONRPCVersion,
					ID:        fmt.Sprintf("w%d-m%d", workerID, m),
					Method:    "session.prompt",
					Params:    json.RawMessage(fmt.Sprintf(`{"worker":%d,"seq":%d}`, workerID, seq)),
					SeqID:     seq,
					Timestamp: time.Now().UTC(),
				}

				if err := clientFramed.WriteEnvelope(env); err != nil {
					t.Errorf("worker %d write failed on message %d: %v", workerID, m, err)
					return
				}
			}
		}(w)
	}

	// Wait for all 10,000 envelopes to be serialized and written to socket/pipe buffer
	writeWg.Wait()

	// Await full reception of 10,000 responses with a generous 30s timeout
	select {
	case <-clientReadDone:
		if clientReadErr != nil {
			t.Fatalf("client reader failure: %v", clientReadErr)
		}
	case <-time.After(30 * time.Second):
		t.Fatalf("timeout: received only %d of %d responses", receivedCount.Load(), totalMessages)
	}

	elapsed := time.Since(startTime)
	rate := float64(totalMessages) / elapsed.Seconds()
	t.Logf("Successfully exchanged %d framed messages in %v (%.0f msgs/sec)", totalMessages, elapsed, rate)

	// Close client framed connection to release server reader
	_ = clientFramed.Close()

	// Wait for server goroutine to terminate cleanly
	select {
	case <-serverDone:
		if serverErr != nil {
			t.Fatalf("server terminated with error: %v", serverErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for server shutdown")
	}
}
