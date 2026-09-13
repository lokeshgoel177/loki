package events_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/lokeshgoel177/loki/internal/events"
	"github.com/lokeshgoel177/loki/internal/protocol"
)

func makeEnvelope(seq uint64, topic string) *protocol.MessageEnvelope {
	return &protocol.MessageEnvelope{
		JSONRPC:   protocol.JSONRPCVersion,
		Method:    topic,
		SeqID:     seq,
		SessionID: "sess-test",
		Timestamp: time.Now().UTC(),
	}
}

func TestNewRingBuffer_Capacity(t *testing.T) {
	// Custom capacity
	rb := events.NewRingBuffer(25)
	if rb.Capacity() != 25 {
		t.Errorf("Capacity() = %d; want 25", rb.Capacity())
	}
	if rb.Size() != 0 {
		t.Errorf("Size() = %d; want 0", rb.Size())
	}

	// Non-positive capacity defaults to DefaultRingBufferCapacity (100)
	rbDefault := events.NewRingBuffer(0)
	if rbDefault.Capacity() != events.DefaultRingBufferCapacity {
		t.Errorf("Capacity() = %d; want %d", rbDefault.Capacity(), events.DefaultRingBufferCapacity)
	}

	rbNegative := events.NewRingBuffer(-10)
	if rbNegative.Capacity() != events.DefaultRingBufferCapacity {
		t.Errorf("Capacity() = %d; want %d", rbNegative.Capacity(), events.DefaultRingBufferCapacity)
	}
}

func TestRingBuffer_AppendAndEvents(t *testing.T) {
	rb := events.NewRingBuffer(5)

	// Initially empty
	if eventsList := rb.Events(); len(eventsList) != 0 {
		t.Fatalf("expected empty events slice, got len %d", len(eventsList))
	}

	// Appending nil should be a no-op
	rb.Append(nil)
	if rb.Size() != 0 {
		t.Fatalf("expected size 0 after appending nil, got %d", rb.Size())
	}

	// Append 3 items (under capacity)
	e1 := makeEnvelope(1, "topic.1")
	e2 := makeEnvelope(2, "topic.2")
	e3 := makeEnvelope(3, "topic.3")

	rb.Append(e1)
	rb.Append(e2)
	rb.Append(e3)

	if rb.Size() != 3 {
		t.Fatalf("Size() = %d; want 3", rb.Size())
	}

	eventsList := rb.Events()
	if len(eventsList) != 3 {
		t.Fatalf("len(Events()) = %d; want 3", len(eventsList))
	}
	if eventsList[0].SeqID != 1 || eventsList[1].SeqID != 2 || eventsList[2].SeqID != 3 {
		t.Errorf("unexpected event ordering: got [%d, %d, %d], want [1, 2, 3]",
			eventsList[0].SeqID, eventsList[1].SeqID, eventsList[2].SeqID)
	}
}

func TestRingBuffer_WrapAround(t *testing.T) {
	cap := 3
	rb := events.NewRingBuffer(cap)

	// Fill to capacity
	rb.Append(makeEnvelope(1, "topic.1"))
	rb.Append(makeEnvelope(2, "topic.2"))
	rb.Append(makeEnvelope(3, "topic.3"))

	if rb.Size() != cap {
		t.Fatalf("Size() = %d; want %d", rb.Size(), cap)
	}

	// Wrap around once: item 4 overwrites item 1
	rb.Append(makeEnvelope(4, "topic.4"))
	if rb.Size() != cap {
		t.Fatalf("Size() after wrap = %d; want %d", rb.Size(), cap)
	}

	res := rb.Events()
	if len(res) != cap {
		t.Fatalf("len(res) = %d; want %d", len(res), cap)
	}
	if res[0].SeqID != 2 || res[1].SeqID != 3 || res[2].SeqID != 4 {
		t.Errorf("unexpected wrap-around ordering: got [%d, %d, %d]; want [2, 3, 4]",
			res[0].SeqID, res[1].SeqID, res[2].SeqID)
	}

	// Wrap around second time: item 5 overwrites item 2
	rb.Append(makeEnvelope(5, "topic.5"))
	res = rb.Events()
	if res[0].SeqID != 3 || res[1].SeqID != 4 || res[2].SeqID != 5 {
		t.Errorf("unexpected second wrap-around ordering: got [%d, %d, %d]; want [3, 4, 5]",
			res[0].SeqID, res[1].SeqID, res[2].SeqID)
	}
}

func TestRingBuffer_Replay(t *testing.T) {
	rb := events.NewRingBuffer(4)

	// Append 6 items (will wrap around with cap 4) -> should retain [3, 4, 5, 6]
	for i := uint64(1); i <= 6; i++ {
		rb.Append(makeEnvelope(i*10, fmt.Sprintf("topic.%d", i)))
	}

	// Replay with afterSeqID = 0 returns all remaining buffered items: [30, 40, 50, 60]
	all := rb.Replay(0)
	if len(all) != 4 {
		t.Fatalf("len(Replay(0)) = %d; want 4", len(all))
	}
	if all[0].SeqID != 30 || all[3].SeqID != 60 {
		t.Errorf("unexpected Replay(0) bounds: first=%d, last=%d", all[0].SeqID, all[3].SeqID)
	}

	// Replay with afterSeqID = 45 should return only items with SeqID > 45: [50, 60]
	filtered := rb.Replay(45)
	if len(filtered) != 2 {
		t.Fatalf("len(Replay(45)) = %d; want 2", len(filtered))
	}
	if filtered[0].SeqID != 50 || filtered[1].SeqID != 60 {
		t.Errorf("unexpected filtered replay: got [%d, %d]; want [50, 60]",
			filtered[0].SeqID, filtered[1].SeqID)
	}

	// Replay with afterSeqID matching or exceeding newest item -> empty slice
	empty := rb.Replay(60)
	if len(empty) != 0 {
		t.Fatalf("len(Replay(60)) = %d; want 0", len(empty))
	}
}

func TestRingBuffer_Clear(t *testing.T) {
	rb := events.NewRingBuffer(3)
	rb.Append(makeEnvelope(1, "topic.1"))
	rb.Append(makeEnvelope(2, "topic.2"))

	if rb.Size() != 2 {
		t.Fatalf("Size() before clear = %d; want 2", rb.Size())
	}

	rb.Clear()

	if rb.Size() != 0 {
		t.Errorf("Size() after clear = %d; want 0", rb.Size())
	}
	if len(rb.Events()) != 0 {
		t.Errorf("Events() after clear is not empty: %v", rb.Events())
	}
}

func TestRingBuffer_ConcurrentAccess(t *testing.T) {
	rb := events.NewRingBuffer(50)
	const numWriters = 4
	const numReaders = 4
	const itemsPerWriter = 200

	var wg sync.WaitGroup
	wg.Add(numWriters + numReaders)

	// Launch concurrent writers
	for w := 0; w < numWriters; w++ {
		go func(writerID int) {
			defer wg.Done()
			for i := 0; i < itemsPerWriter; i++ {
				seq := uint64(writerID*itemsPerWriter + i + 1)
				rb.Append(makeEnvelope(seq, fmt.Sprintf("worker.%d.event", writerID)))
			}
		}(w)
	}

	// Launch concurrent readers
	for r := 0; r < numReaders; r++ {
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				_ = rb.Events()
				_ = rb.Replay(10)
				_ = rb.Size()
				time.Sleep(100 * time.Microsecond)
			}
		}()
	}

	wg.Wait()

	if rb.Size() != 50 {
		t.Errorf("expected buffer to be full (size 50), got %d", rb.Size())
	}
}
