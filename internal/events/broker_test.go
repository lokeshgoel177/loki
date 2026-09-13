package events_test

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/lokeshgoel177/loki/internal/events"
	"github.com/lokeshgoel177/loki/internal/protocol"
)

func TestEventBroker_SubscribeAndUnsubscribe(t *testing.T) {
	broker := events.NewEventBroker()

	if broker.SubscriberCount() != 0 {
		t.Fatalf("expected 0 subscribers, got %d", broker.SubscriberCount())
	}

	// Subscribe client 1
	sub1 := broker.Subscribe("client-1", []string{"session.1.*"})
	if broker.SubscriberCount() != 1 {
		t.Fatalf("expected 1 subscriber, got %d", broker.SubscriberCount())
	}
	if sub1.ID != "client-1" {
		t.Errorf("expected ID 'client-1', got %s", sub1.ID)
	}
	if sub1.IsClosed() {
		t.Errorf("new subscriber should not be marked closed")
	}

	// Re-subscribing same ID should replace the old one and close old queue
	sub1New := broker.Subscribe("client-1", []string{">"})
	if broker.SubscriberCount() != 1 {
		t.Fatalf("expected 1 subscriber after re-subscribe, got %d", broker.SubscriberCount())
	}
	if !sub1.IsClosed() {
		t.Errorf("replaced subscriber should be marked closed")
	}

	// Drain check on old queue
	_, ok := <-sub1.Queue
	if ok {
		t.Errorf("expected replaced subscriber queue to be closed")
	}

	// Unsubscribe
	broker.Unsubscribe("client-1")
	if broker.SubscriberCount() != 0 {
		t.Fatalf("expected 0 subscribers after unsubscribe, got %d", broker.SubscriberCount())
	}
	if !sub1New.IsClosed() {
		t.Errorf("unsubscribed subscriber should be marked closed")
	}

	// Unsubscribing non-existent subscriber should not panic
	broker.Unsubscribe("unknown-client")
}

func TestEventBroker_PublishAndFiltering(t *testing.T) {
	broker := events.NewEventBroker()

	subA := broker.Subscribe("sub-a", []string{"session.123.>"})
	subB := broker.Subscribe("sub-b", []string{"session.*.tool.*"})
	subC := broker.Subscribe("sub-c", []string{">"})
	subD := broker.Subscribe("sub-d", []string{"other.topic"})

	type testPayload struct {
		Text string `json:"text"`
	}

	// 1. Publish to session.123.message.delta (Matches A and C)
	env1, err := broker.Publish("session.123.message.delta", "123", testPayload{Text: "chunk 1"})
	if err != nil {
		t.Fatalf("Publish failed: %v", err)
	}
	if env1.SeqID != 1 {
		t.Errorf("expected SeqID 1, got %d", env1.SeqID)
	}

	select {
	case msg := <-subA.Queue:
		if msg.Method != "session.123.message.delta" {
			t.Errorf("subA received wrong method: %s", msg.Method)
		}
	case <-time.After(100 * time.Millisecond):
		t.Errorf("subA did not receive event")
	}

	select {
	case msg := <-subC.Queue:
		if msg.Method != "session.123.message.delta" {
			t.Errorf("subC received wrong method: %s", msg.Method)
		}
	case <-time.After(100 * time.Millisecond):
		t.Errorf("subC did not receive event")
	}

	// subB and subD should NOT have received this event
	if len(subB.Queue) != 0 {
		t.Errorf("subB unexpectedly received event")
	}
	if len(subD.Queue) != 0 {
		t.Errorf("subD unexpectedly received event")
	}

	// 2. Publish to session.123.tool.started (Matches A, B, and C)
	_, err = broker.Publish("session.123.tool.started", "123", []byte(`{"tool": "grep"}`))
	if err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	// Check subB received it
	select {
	case msg := <-subB.Queue:
		if msg.Method != "session.123.tool.started" {
			t.Errorf("subB received wrong method: %s", msg.Method)
		}
	case <-time.After(100 * time.Millisecond):
		t.Errorf("subB did not receive tool event")
	}
}

func TestEventBroker_PublishPayloadVariants(t *testing.T) {
	broker := events.NewEventBroker()
	sub := broker.Subscribe("sub", []string{">"})

	// Nil payload
	envNil, err := broker.Publish("test.nil", "", nil)
	if err != nil || envNil.Params != nil {
		t.Errorf("expected nil params for nil payload, got %v", envNil.Params)
	}
	<-sub.Queue

	// RawMessage payload
	raw := json.RawMessage(`{"status":"ok"}`)
	envRaw, err := broker.Publish("test.raw", "", raw)
	if err != nil || string(envRaw.Params) != string(raw) {
		t.Errorf("expected raw params %s, got %s", string(raw), string(envRaw.Params))
	}
	<-sub.Queue

	// Byte slice payload
	bytesPayload := []byte(`{"count":42}`)
	envBytes, err := broker.Publish("test.bytes", "", bytesPayload)
	if err != nil || string(envBytes.Params) != string(bytesPayload) {
		t.Errorf("expected byte slice params %s, got %s", string(bytesPayload), string(envBytes.Params))
	}
	<-sub.Queue

	// Unmarshalable payload should error
	_, errInvalid := broker.Publish("test.err", "", make(chan int))
	if errInvalid == nil {
		t.Errorf("expected error for unmarshalable payload, got nil")
	}
}

func TestEventBroker_PublishEnvelopeDefaults(t *testing.T) {
	broker := events.NewEventBroker()
	sub := broker.Subscribe("sub", []string{">"})

	// PublishEnvelope with nil should not panic
	broker.PublishEnvelope(nil)

	// PublishEnvelope with zero fields should populate defaults
	env := &protocol.MessageEnvelope{
		Method: "test.topic",
	}
	broker.PublishEnvelope(env)

	if env.SeqID == 0 {
		t.Errorf("expected non-zero SeqID")
	}
	if env.Timestamp.IsZero() {
		t.Errorf("expected non-zero Timestamp")
	}
	if env.JSONRPC != protocol.JSONRPCVersion {
		t.Errorf("expected JSONRPC %s, got %s", protocol.JSONRPCVersion, env.JSONRPC)
	}

	select {
	case received := <-sub.Queue:
		if received.Method != "test.topic" {
			t.Errorf("unexpected method: %s", received.Method)
		}
	case <-time.After(100 * time.Millisecond):
		t.Errorf("did not receive published envelope")
	}
}

func TestEventBroker_SessionRingBufferIntegration(t *testing.T) {
	broker := events.NewEventBroker()
	sessionID := "session-alpha"

	// Initialize session buffer with capacity 5
	rb := broker.InitSessionBuffer(sessionID, 5)
	if rb == nil {
		t.Fatalf("expected non-nil RingBuffer")
	}

	// Publish 4 events
	for i := 1; i <= 4; i++ {
		_, err := broker.Publish("session.alpha.event", sessionID, map[string]int{"index": i})
		if err != nil {
			t.Fatalf("publish failed: %v", err)
		}
	}

	// Get full replay
	all := broker.GetReplay(sessionID, 0)
	if len(all) != 4 {
		t.Fatalf("expected 4 replay events, got %d", len(all))
	}

	// Get partial replay after SeqID 2
	partial := broker.GetReplay(sessionID, 2)
	if len(partial) != 2 {
		t.Fatalf("expected 2 replay events after SeqID 2, got %d", len(partial))
	}

	// Replay for unknown session should return nil
	if unknown := broker.GetReplay("unknown-session", 0); unknown != nil {
		t.Errorf("expected nil for unknown session replay, got %v", unknown)
	}

	// Remove session buffer
	broker.RemoveSessionBuffer(sessionID)
	if cleared := broker.GetReplay(sessionID, 0); cleared != nil {
		t.Errorf("expected nil replay after removal, got %v", cleared)
	}

	// Removing again should not panic
	broker.RemoveSessionBuffer(sessionID)
}

func TestEventBroker_SlowConsumerNonBlockingDrop(t *testing.T) {
	broker := events.NewEventBroker()
	sub := broker.Subscribe("slow-client", []string{">"})

	totalEvents := events.DefaultSubscriberQueueCapacity + 50

	// Publish more events than queue capacity without reading from the channel
	for i := 1; i <= totalEvents; i++ {
		_, err := broker.Publish("burst.event", "", fmt.Sprintf("event-%d", i))
		if err != nil {
			t.Fatalf("publish failed: %v", err)
		}
	}

	// Verify that exactly 50 events were dropped and never blocked
	if sub.DroppedCount() != 50 {
		t.Errorf("expected 50 dropped events, got %d", sub.DroppedCount())
	}

	// Channel should hold exactly DefaultSubscriberQueueCapacity (256)
	if len(sub.Queue) != events.DefaultSubscriberQueueCapacity {
		t.Errorf("expected queue length %d, got %d", events.DefaultSubscriberQueueCapacity, len(sub.Queue))
	}

	// Drain queue
	drained := 0
	for len(sub.Queue) > 0 {
		<-sub.Queue
		drained++
	}
	if drained != events.DefaultSubscriberQueueCapacity {
		t.Errorf("expected %d drained events, got %d", events.DefaultSubscriberQueueCapacity, drained)
	}
}

func TestEventBroker_ConcurrentPubSub(t *testing.T) {
	broker := events.NewEventBroker()
	const numSubscribers = 5
	const numPublishers = 5
	const eventsPerPublisher = 100

	var subs []*events.Subscriber
	for i := 0; i < numSubscribers; i++ {
		sub := broker.Subscribe(fmt.Sprintf("concurrent-sub-%d", i), []string{">"})
		subs = append(subs, sub)
	}

	var wg sync.WaitGroup
	wg.Add(numPublishers + numSubscribers)

	// Launch concurrent publishers
	for p := 0; p < numPublishers; p++ {
		go func(pubID int) {
			defer wg.Done()
			for i := 0; i < eventsPerPublisher; i++ {
				_, _ = broker.Publish("concurrent.event", "", map[string]int{"pub": pubID, "seq": i})
			}
		}(p)
	}

	// Launch concurrent readers
	for s := 0; s < numSubscribers; s++ {
		go func(sub *events.Subscriber) {
			defer wg.Done()
			readCount := 0
			// Read up to expected events with a short timeout per read
			for {
				select {
				case <-sub.Queue:
					readCount++
					if readCount+int(sub.DroppedCount()) >= numPublishers*eventsPerPublisher {
						return
					}
				case <-time.After(200 * time.Millisecond):
					return
				}
			}
		}(subs[s])
	}

	wg.Wait()
}
