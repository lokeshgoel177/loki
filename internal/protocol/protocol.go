package protocol

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

const (
	MaxFramePayloadSize uint32 = 16 * 1024 * 1024
	JSONRPCVersion      string = "2.0"
)

const (
	ErrCodeParseError     = -32700
	ErrCodeInvalidRequest = -32600
	ErrCodeMethodNotFound = -32601
	ErrCodeInvalidParams  = -32602
	ErrCodeInternalError  = -32603
)

type MessageKind uint8

const (
	KindInvalid MessageKind = iota
	KindRequest
	KindNotification
	KindResponse
)

type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message)
}

// MessageEnvelope represents a framed JSON-RPC 2.0 request, response, or streaming notification.
type MessageEnvelope struct {
	JSONRPC   string          `json:"jsonrpc"`              // Always "2.0"
	ID        string          `json:"id,omitempty"`         // Client/server request ID; omitted on notifications
	Method    string          `json:"method,omitempty"`     // RPC method or Pub/Sub topic (e.g."session.prompt", "message.delta")
	Params    json.RawMessage `json:"params,omitempty"`     // Request arguments or event data
	Result    json.RawMessage `json:"result,omitempty"`     // Successful RPC result
	Error     *RPCError       `json:"error,omitempty"`      // Error response object if failed
	SeqID     uint64          `json:"seq_id,omitempty"`     // Monotonically increasing sequence ID for replay/rehydration
	SessionID string          `json:"session_id,omitempty"` // Target or originating session
	Timestamp time.Time       `json:"timestamp"`            // UTC emission timestamp
}

func (env *MessageEnvelope) Kind() MessageKind {
	if env == nil {
		return KindInvalid
	}
	switch {
	case env.Method != "" && env.ID != "":
		return KindRequest
	case env.Method != "" && env.ID == "":
		return KindNotification
	case env.Method == "" && env.ID != "":
		return KindResponse
	default:
		return KindInvalid
	}
}

// Convenience predicates built on top of Kind()
func (env *MessageEnvelope) IsRequest() bool      { return env.Kind() == KindRequest }
func (env *MessageEnvelope) IsNotification() bool { return env.Kind() == KindNotification }
func (env *MessageEnvelope) IsResponse() bool     { return env.Kind() == KindResponse }

type FramedConn struct {
	conn    net.Conn
	writeMu sync.Mutex
}

func NewFramedConn(c net.Conn) *FramedConn {
	return &FramedConn{conn: c}
}

// WriteEnvelope serializes an envelope with a 4-byte length prefix.
func (fc *FramedConn) WriteEnvelope(env *MessageEnvelope) error {
	msg := *env
	if msg.JSONRPC == "" {
		msg.JSONRPC = JSONRPCVersion
	}
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now().UTC()
	}

	data, err := json.Marshal(&msg)
	if err != nil {
		return fmt.Errorf("protocol: marshal failed: %w", err)
	}

	length := uint32(len(data))
	if length > MaxFramePayloadSize {
		return fmt.Errorf("protocol: payload size %d exceeds 16MB limit", length)
	}

	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], length)

	fc.writeMu.Lock()
	defer fc.writeMu.Unlock()

	buffers := net.Buffers{lenBuf[:], data}
	if _, err := buffers.WriteTo(fc.conn); err != nil {
		return fmt.Errorf("protocol: write failed: %w", err)
	}
	return nil
}

// ReadEnvelope reads one complete length-prefixed MessageEnvelope.
// Note: ReadEnvelope is designed to be called by a single dedicated reader goroutine.
func (fc *FramedConn) ReadEnvelope() (*MessageEnvelope, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(fc.conn, lenBuf[:]); err != nil {
		return nil, err
	}

	length := binary.BigEndian.Uint32(lenBuf[:])
	if length == 0 {
		return nil, fmt.Errorf("protocol: received invalid empty frame")
	}
	if length > MaxFramePayloadSize {
		return nil, fmt.Errorf("protocol: frame length %d exceeds max permitted 16MB", length)
	}

	buf := make([]byte, length)
	if _, err := io.ReadFull(fc.conn, buf); err != nil {
		return nil, err
	}

	var env MessageEnvelope
	if err := json.Unmarshal(buf, &env); err != nil {
		return nil, fmt.Errorf("protocol: unmarshal frame payload failed: %w", err)
	}

	if env.JSONRPC != JSONRPCVersion {
		return nil, fmt.Errorf("protocol: invalid jsonrpc version %q (expected %q)", env.JSONRPC, JSONRPCVersion)
	}

	return &env, nil
}

// Close closes the underlying connection.
func (fc *FramedConn) Close() error {
	return fc.conn.Close()
}
