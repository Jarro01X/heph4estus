package main

import (
	"context"
	"encoding/json"
	"errors"
	"heph4estus/internal/cloud"
	appconfig "heph4estus/internal/config"
	"heph4estus/internal/tools/nmap"
	"strings"
	"testing"
	"time"
)

// mockQueue records which methods were called and returns configured values.
type mockQueue struct {
	msg     *cloud.Message
	deleted bool
}

func (q *mockQueue) Send(ctx context.Context, queueID, body string) error {
	return nil
}
func (q *mockQueue) SendBatch(ctx context.Context, queueID string, bodies []string) error {
	return nil
}
func (q *mockQueue) Receive(ctx context.Context, queueID string) (*cloud.Message, error) {
	return q.msg, nil
}
func (q *mockQueue) Delete(ctx context.Context, queueID, receiptHandle string) error {
	q.deleted = true
	return nil
}

// mockStorage can be configured to fail on Upload.
type mockStorage struct {
	uploadErr error
	uploaded  bool
}

func (s *mockStorage) Upload(ctx context.Context, bucket, key string, data []byte) error {
	if s.uploadErr != nil {
		return s.uploadErr
	}
	s.uploaded = true
	return nil
}
func (s *mockStorage) Download(ctx context.Context, bucket, key string) ([]byte, error) {
	return nil, nil
}
func (s *mockStorage) List(ctx context.Context, bucket, prefix string) ([]string, error) {
	return nil, nil
}
func (s *mockStorage) Count(ctx context.Context, bucket, prefix string) (int, error) {
	return 0, nil
}

type mockLogger struct{}

func (l *mockLogger) Info(format string, args ...interface{})  {}
func (l *mockLogger) Error(format string, args ...interface{}) {}
func (l *mockLogger) Fatal(format string, args ...interface{}) {}

// mockScanner returns preconfigured results and captures the task it received.
type mockScanner struct {
	result      nmap.ScanResult
	capturedTask nmap.ScanTask
}

func (s *mockScanner) RunScan(task nmap.ScanTask) nmap.ScanResult {
	s.capturedTask = task
	r := s.result
	if r.Target == "" {
		r.Target = task.Target
	}
	if r.Timestamp.IsZero() {
		r.Timestamp = time.Now()
	}
	return r
}

func (s *mockScanner) FormatResult(result nmap.ScanResult) ([]byte, error) {
	return json.Marshal(result)
}

func validTaskMessage() *cloud.Message {
	task := nmap.ScanTask{Target: "127.0.0.1", Options: "-sn"}
	body, _ := json.Marshal(task)
	return &cloud.Message{
		ID:            "msg-1",
		Body:          string(body),
		ReceiptHandle: "receipt-1",
		ReceiveCount:  1,
	}
}

func testConfig() *appconfig.ConsumerConfig {
	return &appconfig.ConsumerConfig{
		QueueURL: "https://sqs.us-east-1.amazonaws.com/123456789/test-queue",
		S3Bucket: "test-bucket",
	}
}

func TestProcessMessage_NoDeleteOnUploadFailure(t *testing.T) {
	q := &mockQueue{msg: validTaskMessage()}
	s := &mockStorage{uploadErr: errors.New("S3 unavailable")}
	sc := &mockScanner{result: nmap.ScanResult{Output: "scan output"}}

	processed, err := processMessage(
		context.Background(),
		&mockLogger{},
		testConfig(),
		q,
		s,
		sc,
	)

	if !processed {
		t.Fatal("expected message to be processed")
	}
	if err == nil {
		t.Fatal("expected error from failed upload")
	}
	if q.deleted {
		t.Fatal("message was deleted despite upload failure — retry contract violated")
	}
}

func TestProcessMessage_DeleteAfterSuccessfulUpload(t *testing.T) {
	q := &mockQueue{msg: validTaskMessage()}
	s := &mockStorage{}
	sc := &mockScanner{result: nmap.ScanResult{Output: "scan output"}}

	processed, err := processMessage(
		context.Background(),
		&mockLogger{},
		testConfig(),
		q,
		s,
		sc,
	)

	if !processed {
		t.Fatal("expected message to be processed")
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !s.uploaded {
		t.Fatal("expected result to be uploaded")
	}
	if !q.deleted {
		t.Fatal("expected message to be deleted after successful upload")
	}
}

func TestProcessMessage_EmptyQueue(t *testing.T) {
	q := &mockQueue{msg: nil}
	s := &mockStorage{}
	sc := &mockScanner{}

	processed, _ := processMessage(
		context.Background(),
		&mockLogger{},
		testConfig(),
		q,
		s,
		sc,
	)

	if processed {
		t.Fatal("expected no processing on empty queue")
	}
}

func TestProcessMessage_MalformedMessageDeleted(t *testing.T) {
	q := &mockQueue{
		msg: &cloud.Message{
			ID:            "msg-bad",
			Body:          "not valid json",
			ReceiptHandle: "receipt-bad",
		},
	}
	s := &mockStorage{}
	sc := &mockScanner{}

	processed, err := processMessage(
		context.Background(),
		&mockLogger{},
		testConfig(),
		q,
		s,
		sc,
	)

	if !processed {
		t.Fatal("expected message to be processed")
	}
	if err == nil {
		t.Fatal("expected error from malformed message")
	}
	if !q.deleted {
		t.Fatal("malformed messages should be deleted to prevent poison-pill loop")
	}
}

func TestProcessMessage_TransientError_NoUploadNoDelete(t *testing.T) {
	q := &mockQueue{msg: validTaskMessage()}
	s := &mockStorage{}
	sc := &mockScanner{
		result: nmap.ScanResult{
			Output: "Temporary failure in name resolution",
			Error:  "exit status 1",
		},
	}

	processed, err := processMessage(
		context.Background(),
		&mockLogger{},
		testConfig(),
		q,
		s,
		sc,
	)

	if !processed {
		t.Fatal("expected message to be processed")
	}
	if err != nil {
		t.Fatalf("transient errors should not return error: %v", err)
	}
	if s.uploaded {
		t.Fatal("transient errors should NOT upload results")
	}
	if q.deleted {
		t.Fatal("transient errors should NOT delete message — SQS retries")
	}
}

func TestProcessMessage_TransientTimeout_NoUploadNoDelete(t *testing.T) {
	q := &mockQueue{msg: validTaskMessage()}
	s := &mockStorage{}
	sc := &mockScanner{
		result: nmap.ScanResult{
			Error: "scan timed out after 5 minutes",
		},
	}

	processed, err := processMessage(
		context.Background(),
		&mockLogger{},
		testConfig(),
		q,
		s,
		sc,
	)

	if !processed {
		t.Fatal("expected message to be processed")
	}
	if err != nil {
		t.Fatalf("transient errors should not return error: %v", err)
	}
	if s.uploaded {
		t.Fatal("transient timeout should NOT upload")
	}
	if q.deleted {
		t.Fatal("transient timeout should NOT delete")
	}
}

func TestProcessMessage_PermanentError_UploadsAndDeletes(t *testing.T) {
	q := &mockQueue{msg: validTaskMessage()}
	s := &mockStorage{}
	sc := &mockScanner{
		result: nmap.ScanResult{
			Output: "Failed to resolve \"notahost\".",
			Error:  "exit status 1",
		},
	}

	processed, err := processMessage(
		context.Background(),
		&mockLogger{},
		testConfig(),
		q,
		s,
		sc,
	)

	if !processed {
		t.Fatal("expected message to be processed")
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !s.uploaded {
		t.Fatal("permanent errors should upload error result")
	}
	if !q.deleted {
		t.Fatal("permanent errors should delete message")
	}
}

func TestProcessMessage_DNSInjection(t *testing.T) {
	q := &mockQueue{msg: validTaskMessage()}
	s := &mockStorage{}
	sc := &mockScanner{result: nmap.ScanResult{Output: "ok"}}

	cfg := testConfig()
	cfg.DNSServers = "8.8.8.8,8.8.4.4"

	processMessage(context.Background(), &mockLogger{}, cfg, q, s, sc)

	if !strings.Contains(sc.capturedTask.Options, "--dns-servers 8.8.8.8,8.8.4.4") {
		t.Errorf("expected --dns-servers in options, got %q", sc.capturedTask.Options)
	}
}

func TestProcessMessage_TimingInjection(t *testing.T) {
	q := &mockQueue{msg: validTaskMessage()}
	s := &mockStorage{}
	sc := &mockScanner{result: nmap.ScanResult{Output: "ok"}}

	cfg := testConfig()
	cfg.NmapTimingTemplate = "3"

	processMessage(context.Background(), &mockLogger{}, cfg, q, s, sc)

	if !strings.Contains(sc.capturedTask.Options, "-T3") {
		t.Errorf("expected -T3 in options, got %q", sc.capturedTask.Options)
	}
}

func TestProcessMessage_BothDNSAndTiming(t *testing.T) {
	q := &mockQueue{msg: validTaskMessage()}
	s := &mockStorage{}
	sc := &mockScanner{result: nmap.ScanResult{Output: "ok"}}

	cfg := testConfig()
	cfg.DNSServers = "1.1.1.1"
	cfg.NmapTimingTemplate = "2"

	processMessage(context.Background(), &mockLogger{}, cfg, q, s, sc)

	opts := sc.capturedTask.Options
	if !strings.Contains(opts, "--dns-servers 1.1.1.1") {
		t.Errorf("expected --dns-servers in options, got %q", opts)
	}
	if !strings.Contains(opts, "-T2") {
		t.Errorf("expected -T2 in options, got %q", opts)
	}
}
