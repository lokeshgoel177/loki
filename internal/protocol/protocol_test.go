package protocol_test

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/lokeshgoel177/loki/internal/protocol"
)

// TestRPCError_Error verifies that RPCError formats the error string correctly.
func TestRPCError_Error(t *testing.T) {
	err := &protocol.RPCError{
		Code:    protocol.ErrCodeMethodNotFound,
		Message: "Method not found",
	}
	expected := fmt.Sprintf("rpc error %d: %s", protocol.ErrCodeMethodNotFound, "Method not found")
	if err.Error() != expected {
		t.Fatalf("expected error string %q, got %q", expected, err.Error())
	}
}

// TestMessageEnvelope_Kind tests the Kind determination and boolean predicates.
func TestMessageEnvelope_Kind(t *testing.T) {
	tests := []struct {
		name           string
		env            *protocol.MessageEnvelope
		expectedKind   protocol.MessageKind
		isRequest      bool
		isNotification bool
		isResponse     bool
	}{
		{
			name:           "Nil envelope",
			env:            nil,
			expectedKind:   protocol.KindInvalid,
			isRequest:      false,
			isNotification: false,
			isResponse:     false,
		},
		{
			name: "Request with Method and ID",
			env: &protocol.MessageEnvelope{
				Method: "session.prompt",
				ID:     "req-1",
			},
			expectedKind:   protocol.KindRequest,
			isRequest:      true,
			isNotification: false,
			isResponse:     false,
		},
		{
			name: "Notification with Method and empty ID",
			env: &protocol.MessageEnvelope{
				Method: "message.delta",
			},
			expectedKind:   protocol.KindNotification,
			isRequest:      false,
			isNotification: true,
			isResponse:     false,
		},
		{
			name: "Response with ID and empty Method",
			env: &protocol.MessageEnvelope{
				ID: "req-1",
			},
			expectedKind:   protocol.KindResponse,
			isRequest:      false,
			isNotification: false,
			isResponse:     true,
		},
		{
			name: "Invalid with empty Method and empty ID",
			env: &protocol.MessageEnvelope{
				JSONRPC: "2.0",
			},
			expectedKind:   protocol.KindInvalid,
			isRequest:      false,
			isNotification: false,
			isResponse:     false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if k := tc.env.Kind(); k != tc.expectedKind {
				t.Errorf("expected Kind %d, got %d", tc.expectedKind, k)
			}
			if req := tc.env.IsRequest(); req != tc.isRequest {
				t.Errorf("expected IsRequest %v, got %v", tc.isRequest, req)
			}
			if notif := tc.env.IsNotification(); notif != tc.isNotification {
				t.Errorf("expected IsNotification %v, got %v", tc.isNotification, notif)
			}
			if resp := tc.env.IsResponse(); resp != tc.isResponse {
				t.Errorf("expected IsResponse %v, got %v", tc.isResponse, resp)
			}
		})
	}
}

// TestFramedConn_RoundTrip tests framing and bidirectional serialization of envelopes over net.Pipe.
func TestFramedConn_RoundTrip(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	client := protocol.NewFramedConn(clientConn)
	server := protocol.NewFramedConn(serverConn)

	outbound := &protocol.MessageEnvelope{
		ID:        "msg-100",
		Method:    "session.prompt",
		Params:    json.RawMessage(`{"prompt":"hello loki"}`),
		SessionID: "sess-abc",
		SeqID:     42,
	}

	var wg sync.WaitGroup
	wg.Add(1)

	var received *protocol.MessageEnvelope
	var readErr error

	go func() {
		defer wg.Done()
		received, readErr = server.ReadEnvelope()
	}()

	if err := client.WriteEnvelope(outbound); err != nil {
		t.Fatalf("failed to write envelope: %v", err)
	}

	wg.Wait()

	if readErr != nil {
		t.Fatalf("unexpected read error: %v", readErr)
	}

	if received.JSONRPC != protocol.JSONRPCVersion {
		t.Errorf("expected JSONRPC %q, got %q", protocol.JSONRPCVersion, received.JSONRPC)
	}
	if received.ID != outbound.ID {
		t.Errorf("expected ID %q, got %q", outbound.ID, received.ID)
	}
	if received.Method != outbound.Method {
		t.Errorf("expected Method %q, got %q", outbound.Method, received.Method)
	}
	if received.SessionID != outbound.SessionID {
		t.Errorf("expected SessionID %q, got %q", outbound.SessionID, received.SessionID)
	}
	if received.SeqID != outbound.SeqID {
		t.Errorf("expected SeqID %d, got %d", outbound.SeqID, received.SeqID)
	}
	if string(received.Params) != string(outbound.Params) {
		t.Errorf("expected Params %s, got %s", string(outbound.Params), string(received.Params))
	}
	if received.Timestamp.IsZero() {
		t.Errorf("expected non-zero Timestamp")
	}
}

// TestFramedConn_MaxPayloadExceeded_Write verifies WriteEnvelope rejects payloads > 16MB.
func TestFramedConn_MaxPayloadExceeded_Write(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	client := protocol.NewFramedConn(c1)

	// Allocate a payload slightly larger than MaxFramePayloadSize (16MB)
	largeData := bytes.Repeat([]byte("x"), int(protocol.MaxFramePayloadSize)+1)
	env := &protocol.MessageEnvelope{
		Method: "test.large",
		Params: json.RawMessage(largeData),
	}

	err := client.WriteEnvelope(env)
	if err == nil {
		t.Fatalf("expected error when writing payload > 16MB, got nil")
	}
}

// TestFramedConn_MaxPayloadExceeded_Read verifies ReadEnvelope rejects frame length > 16MB.
func TestFramedConn_MaxPayloadExceeded_Read(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	server := protocol.NewFramedConn(c2)

	go func() {
		var lenBuf [4]byte
		binary.BigEndian.PutUint32(lenBuf[:], protocol.MaxFramePayloadSize+1)
		_, _ = c1.Write(lenBuf[:])
	}()

	_, err := server.ReadEnvelope()
	if err == nil {
		t.Fatalf("expected error reading frame header > 16MB, got nil")
	}
}

// TestFramedConn_EmptyFrame_Read verifies ReadEnvelope rejects empty frames (length == 0).
func TestFramedConn_EmptyFrame_Read(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	server := protocol.NewFramedConn(c2)

	go func() {
		var lenBuf [4]byte
		binary.BigEndian.PutUint32(lenBuf[:], 0)
		_, _ = c1.Write(lenBuf[:])
	}()

	_, err := server.ReadEnvelope()
	if err == nil {
		t.Fatalf("expected error reading 0-length frame, got nil")
	}
}

// TestFramedConn_InvalidJSON_Read verifies ReadEnvelope fails gracefully on malformed JSON payloads.
func TestFramedConn_InvalidJSON_Read(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	server := protocol.NewFramedConn(c2)

	go func() {
		badJSON := []byte(`{not-valid-json`)
		var lenBuf [4]byte
		binary.BigEndian.PutUint32(lenBuf[:], uint32(len(badJSON)))
		_, _ = c1.Write(lenBuf[:])
		_, _ = c1.Write(badJSON)
	}()

	_, err := server.ReadEnvelope()
	if err == nil {
		t.Fatalf("expected unmarshal error on bad JSON, got nil")
	}
}

// TestFramedConn_InvalidVersion_Read verifies ReadEnvelope rejects non-2.0 JSON-RPC versions.
func TestFramedConn_InvalidVersion_Read(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	server := protocol.NewFramedConn(c2)

	go func() {
		wrongVersion := []byte(`{"jsonrpc":"1.0","id":"1","method":"test"}`)
		var lenBuf [4]byte
		binary.BigEndian.PutUint32(lenBuf[:], uint32(len(wrongVersion)))
		_, _ = c1.Write(lenBuf[:])
		_, _ = c1.Write(wrongVersion)
	}()

	_, err := server.ReadEnvelope()
	if err == nil {
		t.Fatalf("expected error on invalid jsonrpc version, got nil")
	}
}

// TestFramedConn_ConcurrentWrites verifies mutex protection prevents frame interleaving under load.
func TestFramedConn_ConcurrentWrites(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	client := protocol.NewFramedConn(c1)
	server := protocol.NewFramedConn(c2)

	const numGoroutines = 10
	const msgsPerGoroutine = 50
	totalMessages := numGoroutines * msgsPerGoroutine

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Writer goroutines writing concurrently to client
	for i := 0; i < numGoroutines; i++ {
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < msgsPerGoroutine; j++ {
				env := &protocol.MessageEnvelope{
					ID:        fmt.Sprintf("w%d-m%d", workerID, j),
					Method:    "test.ping",
					Timestamp: time.Now().UTC(),
				}
				if err := client.WriteEnvelope(env); err != nil {
					t.Errorf("worker %d write failed: %v", workerID, err)
					return
				}
			}
		}(i)
	}

	// Reader goroutine reading from server
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		receivedCount := 0
		for receivedCount < totalMessages {
			env, err := server.ReadEnvelope()
			if err != nil {
				t.Errorf("read envelope failed after %d messages: %v", receivedCount, err)
				return
			}
			if env.Method != "test.ping" {
				t.Errorf("unexpected method: %s", env.Method)
			}
			receivedCount++
		}
	}()

	wg.Wait()
	<-readDone
}

// TestFramedConn_Close tests that Close() cleanly terminates the underlying connection.
func TestFramedConn_Close(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c2.Close()

	client := protocol.NewFramedConn(c1)
	if err := client.Close(); err != nil {
		t.Fatalf("unexpected error closing FramedConn: %v", err)
	}

	// Writing to a closed connection must fail
	env := &protocol.MessageEnvelope{Method: "ping"}
	if err := client.WriteEnvelope(env); err == nil {
		t.Fatalf("expected error writing to closed connection, got nil")
	}
}
