package selfhosted

import (
	"context"
	"fmt"
	"sync"
	"time"

	"heph4estus/internal/cloud"
	"heph4estus/internal/logger"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// QueueConfig describes the NATS JetStream settings for the selfhosted queue.
type QueueConfig struct {
	URL            string
	StreamName     string
	DurablePrefix  string
	AckWaitSeconds int
	MaxDeliver     int
}

func (c QueueConfig) ackWait() time.Duration {
	if c.AckWaitSeconds > 0 {
		return time.Duration(c.AckWaitSeconds) * time.Second
	}
	return 30 * time.Second
}

func (c QueueConfig) maxDeliver() int {
	if c.MaxDeliver > 0 {
		return c.MaxDeliver
	}
	return 5
}

func (c QueueConfig) streamName() string {
	if c.StreamName != "" {
		return c.StreamName
	}
	return "heph"
}

func (c QueueConfig) durablePrefix() string {
	if c.DurablePrefix != "" {
		return c.DurablePrefix
	}
	return "heph-worker"
}

// Queue is a cloud.Queue implementation backed by NATS JetStream.
type Queue struct {
	nc     *nats.Conn
	js     jetstream.JetStream
	cfg    QueueConfig
	logger logger.Logger

	mu       sync.Mutex
	inflight map[string]jetstream.Msg
}

// NewQueue connects to NATS and returns a JetStream-backed queue.
func NewQueue(cfg QueueConfig, log logger.Logger) (*Queue, error) {
	if log == nil {
		return nil, fmt.Errorf("selfhosted: logger is required")
	}
	if cfg.URL == "" {
		return nil, fmt.Errorf("selfhosted: NATS URL is required")
	}
	nc, err := nats.Connect(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("selfhosted: nats connect: %w", err)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("selfhosted: jetstream init: %w", err)
	}
	return &Queue{
		nc:       nc,
		js:       js,
		cfg:      cfg,
		logger:   log,
		inflight: make(map[string]jetstream.Msg),
	}, nil
}

// NewQueueFromConn wraps an existing NATS connection. Used by tests that
// spin up an embedded server and want to share the connection.
func NewQueueFromConn(nc *nats.Conn, cfg QueueConfig, log logger.Logger) (*Queue, error) {
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, fmt.Errorf("selfhosted: jetstream init: %w", err)
	}
	return &Queue{
		nc:       nc,
		js:       js,
		cfg:      cfg,
		logger:   log,
		inflight: make(map[string]jetstream.Msg),
	}, nil
}

// subject maps a logical queueID to a NATS subject scoped under the stream.
func (q *Queue) subject(queueID string) string {
	return q.cfg.streamName() + "." + queueID
}

// consumerName returns the durable consumer name for a given queue.
func (q *Queue) consumerName(queueID string) string {
	return q.cfg.durablePrefix() + "-" + queueID
}

// ensureStream creates or updates the stream and durable consumer for
// queueID. JetStream is idempotent so this is safe to call repeatedly.
func (q *Queue) ensureStream(ctx context.Context, queueID string) (jetstream.Consumer, error) {
	stream, err := q.js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     q.cfg.streamName(),
		Subjects: []string{q.cfg.streamName() + ".>"},
	})
	if err != nil {
		return nil, fmt.Errorf("selfhosted: ensure stream: %w", err)
	}
	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       q.consumerName(queueID),
		AckWait:       q.cfg.ackWait(),
		MaxDeliver:    q.cfg.maxDeliver(),
		FilterSubject: q.subject(queueID),
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	if err != nil {
		return nil, fmt.Errorf("selfhosted: ensure consumer: %w", err)
	}
	return consumer, nil
}

func (q *Queue) Send(ctx context.Context, queueID, body string) error {
	q.logger.Info("Publishing message to NATS stream: %s", q.subject(queueID))
	if _, err := q.ensureStream(ctx, queueID); err != nil {
		return err
	}
	_, err := q.js.Publish(ctx, q.subject(queueID), []byte(body))
	return err
}

func (q *Queue) SendBatch(ctx context.Context, queueID string, bodies []string) error {
	q.logger.Info("Publishing %d messages to NATS stream: %s", len(bodies), q.subject(queueID))
	if _, err := q.ensureStream(ctx, queueID); err != nil {
		return err
	}
	for i, body := range bodies {
		if _, err := q.js.Publish(ctx, q.subject(queueID), []byte(body)); err != nil {
			return fmt.Errorf("selfhosted: publish batch offset %d: %w", i, err)
		}
	}
	return nil
}

func (q *Queue) Receive(ctx context.Context, queueID string) (*cloud.Message, error) {
	q.logger.Info("Fetching message from NATS consumer: %s", q.consumerName(queueID))
	consumer, err := q.ensureStream(ctx, queueID)
	if err != nil {
		return nil, err
	}
	batch, err := consumer.Fetch(1, jetstream.FetchMaxWait(2*time.Second))
	if err != nil {
		return nil, fmt.Errorf("selfhosted: fetch: %w", err)
	}
	var msg jetstream.Msg
	for m := range batch.Messages() {
		msg = m
	}
	if batch.Error() != nil {
		return nil, fmt.Errorf("selfhosted: fetch iteration: %w", batch.Error())
	}
	if msg == nil {
		return nil, nil
	}

	meta, err := msg.Metadata()
	if err != nil {
		return nil, fmt.Errorf("selfhosted: message metadata: %w", err)
	}

	token := fmt.Sprintf("%s:%d:%d", q.consumerName(queueID), meta.Sequence.Stream, meta.NumDelivered)
	q.mu.Lock()
	q.inflight[token] = msg
	q.mu.Unlock()

	return &cloud.Message{
		ID:            fmt.Sprintf("%d", meta.Sequence.Stream),
		Body:          string(msg.Data()),
		ReceiptHandle: token,
		ReceiveCount:  int(meta.NumDelivered),
	}, nil
}

func (q *Queue) Delete(ctx context.Context, queueID, receiptHandle string) error {
	q.logger.Info("Acking message via receipt handle: %s", receiptHandle)
	q.mu.Lock()
	msg, ok := q.inflight[receiptHandle]
	if ok {
		delete(q.inflight, receiptHandle)
	}
	q.mu.Unlock()
	if !ok {
		return fmt.Errorf("selfhosted: unknown receipt handle %q", receiptHandle)
	}
	return msg.Ack()
}

// Close drains the NATS connection. Callers should call this on shutdown.
func (q *Queue) Close() {
	q.nc.Close()
}
