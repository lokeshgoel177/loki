package events

import (
	"sync"

	"github.com/lokeshgoel177/loki/internal/protocol"
)

// DefaultRingBufferCapacity defines the standard number of events retained per session.
const DefaultRingBufferCapacity = 100

// RingBuffer implements a thread-safe, bounded, fixed-capacity circular buffer
// storing recent *protocol.MessageEnvelope items. It allows reconnecting clients
// to rehydrate recent session events without hitting SQLite.
type RingBuffer struct {
	mu     sync.RWMutex
	events []*protocol.MessageEnvelope
	head   int // Index where the next element will be written
	size   int // Current number of elements stored (0 <= size <= cap)
	cap    int // Maximum capacity of the ring buffer
}

// NewRingBuffer allocates and initializes a RingBuffer with the given capacity.
// If capacity <= 0, DefaultRingBufferCapacity (100) is used.
func NewRingBuffer(capacity int) *RingBuffer {
	if capacity <= 0 {
		capacity = DefaultRingBufferCapacity
	}
	return &RingBuffer{
		events: make([]*protocol.MessageEnvelope, capacity),
		cap:    capacity,
	}
}

// Append inserts an event into the ring buffer. If the buffer is full,
// the oldest event is overwritten in O(1) time.
func (r *RingBuffer) Append(env *protocol.MessageEnvelope) {
	if env == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	r.events[r.head] = env
	r.head = (r.head + 1) % r.cap
	if r.size < r.cap {
		r.size++
	}
}

// Events returns a snapshot of all events currently in the buffer
// in chronological order (oldest to newest).
func (r *RingBuffer) Events() []*protocol.MessageEnvelope {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]*protocol.MessageEnvelope, 0, r.size)
	// Compute the index of the oldest element:
	// When size < cap, oldest element is at index 0.
	// When buffer wrapped (size == cap), oldest element is at current head.
	start := (r.head - r.size + r.cap) % r.cap
	for i := 0; i < r.size; i++ {
		idx := (start + i) % r.cap
		out = append(out, r.events[idx])
	}
	return out
}

// Replay returns events that have a SeqID strictly greater than afterSeqID,
// in chronological order. If afterSeqID == 0, it returns all buffered events.
func (r *RingBuffer) Replay(afterSeqID uint64) []*protocol.MessageEnvelope {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]*protocol.MessageEnvelope, 0, r.size)
	start := (r.head - r.size + r.cap) % r.cap
	for i := 0; i < r.size; i++ {
		idx := (start + i) % r.cap
		e := r.events[idx]
		if afterSeqID == 0 || e.SeqID > afterSeqID {
			out = append(out, e)
		}
	}
	return out
}

// Size returns the current number of valid events stored in the buffer.
func (r *RingBuffer) Size() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.size
}

// Capacity returns the maximum capacity of the ring buffer.
func (r *RingBuffer) Capacity() int {
	return r.cap
}

// Clear wipes all events and resets buffer indices, allowing the Go GC
// to reclaim envelope memory immediately.
func (r *RingBuffer) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i := range r.events {
		r.events[i] = nil
	}
	r.head = 0
	r.size = 0
}
