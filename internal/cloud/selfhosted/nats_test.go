package selfhosted

import (
	"context"
	"testing"
	"time"

	"heph4estus/internal/logger"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

func startEmbeddedNATS(t *testing.T) *natsserver.Server {
	t.Helper()
	opts := &natsserver.Options{
		Port:      -1,
		JetStream: true,
		StoreDir:  t.TempDir(),
	}
	srv, err := natsserver.NewServer(opts)
	if err != nil {
		t.Fatalf("embedded nats: %v", err)
	}
	srv.Start()
	if !srv.ReadyForConnections(5 * time.Second) {
		t.Fatal("embedded nats not ready")
	}
	t.Cleanup(srv.Shutdown)
	return srv
}

func newTestQueue(t *testing.T) *Queue {
	t.Helper()
	srv := startEmbeddedNATS(t)
	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("nats connect: %v", err)
	}
	t.Cleanup(nc.Close)
	q, err := NewQueueFromConn(nc, QueueConfig{
		StreamName:     "test",
		DurablePrefix:  "test-worker",
		AckWaitSeconds: 2,
		MaxDeliver:     3,
	}, logger.NewSimpleLogger())
	if err != nil {
		t.Fatalf("new queue: %v", err)
	}
	return q
}

func TestSendThenReceive(t *testing.T) {
	q := newTestQueue(t)
	ctx := context.Background()

	if err := q.Send(ctx, "q1", `{"target":"10.0.0.1"}`); err != nil {
		t.Fatalf("send: %v", err)
	}
	msg, err := q.Receive(ctx, "q1")
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if msg == nil {
		t.Fatal("expected message, got nil")
	}
	if msg.Body != `{"target":"10.0.0.1"}` {
		t.Fatalf("body mismatch: %q", msg.Body)
	}
	if msg.ReceiveCount != 1 {
		t.Fatalf("expected ReceiveCount=1, got %d", msg.ReceiveCount)
	}
}

func TestDeleteAcksMessage(t *testing.T) {
	q := newTestQueue(t)
	ctx := context.Background()

	if err := q.Send(ctx, "q2", "hello"); err != nil {
		t.Fatalf("send: %v", err)
	}
	msg, err := q.Receive(ctx, "q2")
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if err := q.Delete(ctx, "q2", msg.ReceiptHandle); err != nil {
		t.Fatalf("delete: %v", err)
	}
	// After ack, queue should be empty.
	msg2, err := q.Receive(ctx, "q2")
	if err != nil {
		t.Fatalf("receive after delete: %v", err)
	}
	if msg2 != nil {
		t.Fatalf("expected nil after ack, got %+v", msg2)
	}
}

func TestReceiveEmptyQueue(t *testing.T) {
	q := newTestQueue(t)
	ctx := context.Background()

	// Ensure stream exists first so we don't get an error on empty fetch.
	if err := q.Send(ctx, "q3", "seed"); err != nil {
		t.Fatalf("seed send: %v", err)
	}
	seed, _ := q.Receive(ctx, "q3")
	if seed != nil {
		_ = q.Delete(ctx, "q3", seed.ReceiptHandle)
	}

	msg, err := q.Receive(ctx, "q3")
	if err != nil {
		t.Fatalf("receive empty: %v", err)
	}
	if msg != nil {
		t.Fatalf("expected nil on empty queue, got %+v", msg)
	}
}

func TestUnackedRedelivery(t *testing.T) {
	q := newTestQueue(t)
	ctx := context.Background()

	if err := q.Send(ctx, "q4", "retry-me"); err != nil {
		t.Fatalf("send: %v", err)
	}
	msg1, err := q.Receive(ctx, "q4")
	if err != nil {
		t.Fatalf("first receive: %v", err)
	}
	if msg1 == nil {
		t.Fatal("expected message on first receive")
	}
	// Do NOT ack (delete). Wait for ack_wait (2s) then receive again.
	// NAK immediately to speed up redelivery.
	q.mu.Lock()
	raw, ok := q.inflight[msg1.ReceiptHandle]
	q.mu.Unlock()
	if !ok {
		t.Fatal("inflight message not found")
	}
	if err := raw.Nak(); err != nil {
		t.Fatalf("nak: %v", err)
	}
	q.mu.Lock()
	delete(q.inflight, msg1.ReceiptHandle)
	q.mu.Unlock()

	msg2, err := q.Receive(ctx, "q4")
	if err != nil {
		t.Fatalf("second receive: %v", err)
	}
	if msg2 == nil {
		t.Fatal("expected redelivery")
	}
	if msg2.Body != "retry-me" {
		t.Fatalf("body mismatch on redelivery: %q", msg2.Body)
	}
	if msg2.ReceiveCount < 2 {
		t.Fatalf("expected ReceiveCount >= 2, got %d", msg2.ReceiveCount)
	}
}

func TestReceiveCountIncrementsOnRedelivery(t *testing.T) {
	q := newTestQueue(t)
	ctx := context.Background()

	if err := q.Send(ctx, "q5", "count-me"); err != nil {
		t.Fatalf("send: %v", err)
	}

	for i := 1; i <= 2; i++ {
		msg, err := q.Receive(ctx, "q5")
		if err != nil {
			t.Fatalf("receive attempt %d: %v", i, err)
		}
		if msg == nil {
			t.Fatalf("nil on attempt %d", i)
		}
		if msg.ReceiveCount != i {
			t.Fatalf("attempt %d: expected ReceiveCount=%d, got %d", i, i, msg.ReceiveCount)
		}
		// NAK to trigger redelivery.
		q.mu.Lock()
		raw := q.inflight[msg.ReceiptHandle]
		delete(q.inflight, msg.ReceiptHandle)
		q.mu.Unlock()
		if err := raw.Nak(); err != nil {
			t.Fatalf("nak attempt %d: %v", i, err)
		}
	}
}

func TestSendBatch(t *testing.T) {
	q := newTestQueue(t)
	ctx := context.Background()

	bodies := make([]string, 25)
	for i := range bodies {
		bodies[i] = "msg"
	}
	if err := q.SendBatch(ctx, "q6", bodies); err != nil {
		t.Fatalf("send batch: %v", err)
	}
	count := 0
	for {
		msg, err := q.Receive(ctx, "q6")
		if err != nil {
			t.Fatalf("receive: %v", err)
		}
		if msg == nil {
			break
		}
		count++
		if err := q.Delete(ctx, "q6", msg.ReceiptHandle); err != nil {
			t.Fatalf("delete: %v", err)
		}
	}
	if count != 25 {
		t.Fatalf("expected 25 messages, got %d", count)
	}
}

func TestDeleteUnknownHandle(t *testing.T) {
	q := newTestQueue(t)
	ctx := context.Background()

	err := q.Delete(ctx, "q7", "bogus-handle")
	if err == nil {
		t.Fatal("expected error for unknown receipt handle")
	}
}
