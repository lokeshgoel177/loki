package events

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lokeshgoel177/loki/internal/protocol"
)

// DefaultSubscriberQueueCapacity defines the channel buffer size per client connection (256 events).
const DefaultSubscriberQueueCapacity = 256

// Subscriber represents an attached client connection receiving a stream of events.
type Subscriber struct {
	ID      string
	Topics  []string
	Queue   chan *protocol.MessageEnvelope
	Dropped atomic.Uint64
	closed  atomic.Bool
}

// DroppedCount returns the total number of events dropped due to channel backpressure.
func (s *Subscriber) DroppedCount() uint64 {
	return s.Dropped.Load()
}

// IsClosed returns true if the subscriber has been unsubscribed.
func (s *Subscriber) IsClosed() bool {
	return s.closed.Load()
}

// EventBroker coordinates pub/sub distribution and session event replay.
type EventBroker struct {
	subMu       sync.RWMutex           // protects subscribers map
	subscribers map[string]*Subscriber

	rbMu        sync.RWMutex           // protects ringBuffers map
	ringBuffers map[string]*RingBuffer // sessionID -> RingBuffer (last 100 events)

	publishMu sync.Mutex   // serializes PublishEnvelope to preserve SeqID ordering
	globalSeq atomic.Uint64
}

// NewEventBroker initializes and returns a ready-to-use EventBroker.
func NewEventBroker() *EventBroker {
	return &EventBroker{
		subscribers: make(map[string]*Subscriber),
		ringBuffers: make(map[string]*RingBuffer),
	}
}

// Subscribe registers a new client with the broker.
func (b *EventBroker) Subscribe(subID string, topics []string) *Subscriber {
	b.subMu.Lock()
	defer b.subMu.Unlock()

	// If a subscriber with this ID already exists, clean up its old channel
	if old, exists := b.subscribers[subID]; exists {
		old.closed.Store(true)
		close(old.Queue)
	}

	sub := &Subscriber{
		ID:     subID,
		Topics: topics,
		Queue:  make(chan *protocol.MessageEnvelope, DefaultSubscriberQueueCapacity),
	}

	b.subscribers[subID] = sub
	return sub
}

// Unsubscribe cleanly removes a client connection and closes its queue.
func (b *EventBroker) Unsubscribe(subID string) {
	b.subMu.Lock()
	defer b.subMu.Unlock()

	sub, exists := b.subscribers[subID]
	if !exists {
		return
	}

	delete(b.subscribers, subID)
	sub.closed.Store(true)
	close(sub.Queue)
}

// SubscriberCount returns the current count of active subscribers.
func (b *EventBroker) SubscriberCount() int {
	b.subMu.RLock()
	defer b.subMu.RUnlock()
	return len(b.subscribers)
}

// InitSessionBuffer allocates a 100-event ring buffer for a newly created session.
func (b *EventBroker) InitSessionBuffer(sessionID string, capacity int) *RingBuffer {
	b.rbMu.Lock()
	defer b.rbMu.Unlock()

	rb := NewRingBuffer(capacity)
	b.ringBuffers[sessionID] = rb
	return rb
}

// RemoveSessionBuffer tears down a session's in-memory ring buffer (used on session eviction/kill).
func (b *EventBroker) RemoveSessionBuffer(sessionID string) {
	b.rbMu.Lock()
	defer b.rbMu.Unlock()

	if rb, exists := b.ringBuffers[sessionID]; exists {
		rb.Clear()
		delete(b.ringBuffers, sessionID)
	}
}

// GetReplay retrieves chronological buffered events for a session after the specified SeqID.
func (b *EventBroker) GetReplay(sessionID string, afterSeqID uint64) []*protocol.MessageEnvelope {
	b.rbMu.RLock()
	defer b.rbMu.RUnlock()

	rb, exists := b.ringBuffers[sessionID]
	if !exists {
		return nil
	}
	return rb.Replay(afterSeqID)
}

// Publish creates and broadcasts an event notification to all matching subscribers.
func (b *EventBroker) Publish(topic string, sessionID string, payload any) (*protocol.MessageEnvelope, error) {
	var rawParams json.RawMessage
	if payload != nil {
		switch p := payload.(type) {
		case json.RawMessage:
			rawParams = p
		case []byte:
			rawParams = json.RawMessage(p)
		default:
			data, err := json.Marshal(p)
			if err != nil {
				return nil, fmt.Errorf("events: failed to marshal payload: %w", err)
			}
			rawParams = json.RawMessage(data)
		}
	}

	env := &protocol.MessageEnvelope{
		JSONRPC:   protocol.JSONRPCVersion,
		Method:    topic,
		Params:    rawParams,
		SessionID: sessionID,
		// SeqID and Timestamp are assigned by PublishEnvelope under publishMu
		// to guarantee monotonic delivery order.
	}

	b.PublishEnvelope(env)
	return env, nil
}

// PublishEnvelope routes a pre-constructed envelope to session ring buffers and matching clients.
// The entire publish path is serialized by publishMu to guarantee that events are delivered
// in monotonically increasing SeqID order.
func (b *EventBroker) PublishEnvelope(env *protocol.MessageEnvelope) {
	if env == nil {
		return
	}

	b.publishMu.Lock()
	defer b.publishMu.Unlock()

	// Fill defaults under the serialization lock so SeqID ordering matches delivery order.
	if env.SeqID == 0 {
		env.SeqID = b.globalSeq.Add(1)
	}
	if env.Timestamp.IsZero() {
		env.Timestamp = time.Now().UTC()
	}
	if env.JSONRPC == "" {
		env.JSONRPC = protocol.JSONRPCVersion
	}

	// Append to session ring buffer if one exists
	if env.SessionID != "" {
		b.rbMu.RLock()
		if rb, ok := b.ringBuffers[env.SessionID]; ok {
			rb.Append(env)
		}
		b.rbMu.RUnlock()
	}

	// Fan out to matching subscribers with non-blocking drop
	b.subMu.RLock()
	for _, sub := range b.subscribers {
		if MatchesAnyTopic(sub.Topics, env.Method) {
			select {
			case sub.Queue <- env:
			default:
				sub.Dropped.Add(1)
			}
		}
	}
	b.subMu.RUnlock()
}
