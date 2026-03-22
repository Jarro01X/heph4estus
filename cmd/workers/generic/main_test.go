package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"heph4estus/internal/cloud"
	appconfig "heph4estus/internal/config"
	"heph4estus/internal/modules"
	"heph4estus/internal/worker"
)

type mockQueue struct {
	msg     *cloud.Message
	deleted bool
}

func (q *mockQueue) Send(ctx context.Context, queueID, body string) error      { return nil }
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

type mockExecutor struct {
	result      worker.Result
	outputBytes []byte
	execErr     error
}

func (e *mockExecutor) Execute(ctx context.Context, mod *modules.ModuleDefinition, task worker.Task) (worker.Result, []byte, error) {
	r := e.result
	if r.Target == "" {
		r.Target = task.Target
	}
	if r.Timestamp.IsZero() {
		r.Timestamp = time.Now()
	}
	return r, e.outputBytes, e.execErr
}

func testConfig() *appconfig.WorkerConfig {
	return &appconfig.WorkerConfig{
		QueueURL: "https://sqs.us-east-1.amazonaws.com/123456789/test-queue",
		S3Bucket: "test-bucket",
		ToolName: "nmap",
	}
}

func testModule() *modules.ModuleDefinition {
	return &modules.ModuleDefinition{
		Name:          "nmap",
		Command:       "nmap {{options}} -oX {{output}} {{target}}",
		InputType:     "target_list",
		OutputExt:     "xml",
		InstallCmd:    "apk add --no-cache nmap",
		DefaultCPU:    256,
		DefaultMemory: 512,
		Timeout:       "5m",
		Tags:          []string{"scanner", "network"},
	}
}

func validTaskMessage() *cloud.Message {
	task := worker.Task{ToolName: "nmap", Target: "127.0.0.1", Options: "-sn"}
	body, _ := json.Marshal(task)
	return &cloud.Message{
		ID:            "msg-1",
		Body:          string(body),
		ReceiptHandle: "receipt-1",
		ReceiveCount:  1,
	}
}

func TestProcessMessage_EmptyQueue(t *testing.T) {
	q := &mockQueue{msg: nil}
	s := &mockStorage{}
	e := &mockExecutor{}

	processed, _ := processMessage(
		context.Background(), &mockLogger{}, testConfig(), testModule(), q, s, e,
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
	e := &mockExecutor{}

	processed, err := processMessage(
		context.Background(), &mockLogger{}, testConfig(), testModule(), q, s, e,
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
	e := &mockExecutor{
		result: worker.Result{
			Output: "Temporary failure in name resolution",
			Error:  "exit status 1",
		},
	}

	processed, err := processMessage(
		context.Background(), &mockLogger{}, testConfig(), testModule(), q, s, e,
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

func TestProcessMessage_PermanentError_UploadsAndDeletes(t *testing.T) {
	q := &mockQueue{msg: validTaskMessage()}
	s := &mockStorage{}
	e := &mockExecutor{
		result: worker.Result{
			Output: "Failed to resolve \"notahost\".",
			Error:  "exit status 1",
		},
	}

	processed, err := processMessage(
		context.Background(), &mockLogger{}, testConfig(), testModule(), q, s, e,
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

func TestProcessMessage_NoDeleteOnUploadFailure(t *testing.T) {
	q := &mockQueue{msg: validTaskMessage()}
	s := &mockStorage{uploadErr: errors.New("S3 unavailable")}
	e := &mockExecutor{
		result: worker.Result{Output: "scan output"},
	}

	processed, err := processMessage(
		context.Background(), &mockLogger{}, testConfig(), testModule(), q, s, e,
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
	e := &mockExecutor{
		result:      worker.Result{Output: "scan output"},
		outputBytes: []byte("<xml>results</xml>"),
	}

	processed, err := processMessage(
		context.Background(), &mockLogger{}, testConfig(), testModule(), q, s, e,
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

func TestProcessMessage_ExecutionError(t *testing.T) {
	q := &mockQueue{msg: validTaskMessage()}
	s := &mockStorage{}
	e := &mockExecutor{
		execErr: errors.New("failed to create temp dir"),
	}

	processed, err := processMessage(
		context.Background(), &mockLogger{}, testConfig(), testModule(), q, s, e,
	)

	if !processed {
		t.Fatal("expected message to be processed")
	}
	if err == nil {
		t.Fatal("expected error from execution failure")
	}
	if q.deleted {
		t.Fatal("message should not be deleted on execution error — SQS retries")
	}
}
