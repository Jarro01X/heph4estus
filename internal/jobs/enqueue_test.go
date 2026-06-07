package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"heph4estus/internal/cloud"
	"heph4estus/internal/worker"
)

var _ cloud.Queue = (*enqueueRecordingQueue)(nil)

type enqueueRecordingQueue struct {
	mu sync.Mutex

	batches  [][]string
	queueIDs []string

	failCall int
	sendErr  error

	active    int
	maxActive int
	started   chan struct{}
	release   <-chan struct{}
}

func (q *enqueueRecordingQueue) Send(context.Context, string, string) error { return nil }

func (q *enqueueRecordingQueue) SendBatch(ctx context.Context, queueID string, bodies []string) error {
	copied := append([]string(nil), bodies...)

	q.mu.Lock()
	call := len(q.batches)
	q.batches = append(q.batches, copied)
	q.queueIDs = append(q.queueIDs, queueID)
	q.active++
	if q.active > q.maxActive {
		q.maxActive = q.active
	}
	q.mu.Unlock()

	if q.started != nil {
		q.started <- struct{}{}
	}

	defer func() {
		q.mu.Lock()
		q.active--
		q.mu.Unlock()
	}()

	if q.release != nil {
		select {
		case <-q.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	if q.failCall == call {
		return q.sendErr
	}
	return nil
}

func (q *enqueueRecordingQueue) Receive(context.Context, string) (*cloud.Message, error) {
	return nil, nil
}

func (q *enqueueRecordingQueue) Delete(context.Context, string, string) error { return nil }

func makeEnqueueTasks(n int) []worker.Task {
	tasks := make([]worker.Task, n)
	for i := range tasks {
		tasks[i] = worker.Task{
			ToolName: "httpx",
			JobID:    "job-123",
			Target:   fmt.Sprintf("target-%03d.example.com", i),
			Options:  "-silent",
		}
	}
	return tasks
}

func TestEnqueueTasksEmpty(t *testing.T) {
	queue := &enqueueRecordingQueue{}
	result, err := EnqueueTasks(context.Background(), queue, "queue-url", nil, EnqueueOptions{})
	if err != nil {
		t.Fatalf("enqueue tasks: %v", err)
	}
	if result.TotalTasks != 0 || result.SentTasks != 0 || result.BatchCount != 0 {
		t.Fatalf("result = %#v, want zero counts", result)
	}
	if len(queue.batches) != 0 {
		t.Fatalf("queue batches = %d, want 0", len(queue.batches))
	}
}

func TestEnqueueTasksSplitsDefaultBatches(t *testing.T) {
	queue := &enqueueRecordingQueue{}
	result, err := EnqueueTasks(context.Background(), queue, "queue-url", makeEnqueueTasks(25), EnqueueOptions{
		Concurrency: 1,
	})
	if err != nil {
		t.Fatalf("enqueue tasks: %v", err)
	}
	if result.TotalTasks != 25 || result.SentTasks != 25 || result.BatchCount != 3 {
		t.Fatalf("result = %#v, want total=25 sent=25 batches=3", result)
	}

	wantSizes := []int{10, 10, 5}
	if len(queue.batches) != len(wantSizes) {
		t.Fatalf("batches = %d, want %d", len(queue.batches), len(wantSizes))
	}
	for i, want := range wantSizes {
		if got := len(queue.batches[i]); got != want {
			t.Fatalf("batch %d size = %d, want %d", i, got, want)
		}
		if queue.queueIDs[i] != "queue-url" {
			t.Fatalf("batch %d queueID = %q, want queue-url", i, queue.queueIDs[i])
		}
	}

	var first, last worker.Task
	if err := json.Unmarshal([]byte(queue.batches[0][0]), &first); err != nil {
		t.Fatalf("unmarshal first task: %v", err)
	}
	if err := json.Unmarshal([]byte(queue.batches[2][4]), &last); err != nil {
		t.Fatalf("unmarshal last task: %v", err)
	}
	if first.Target != "target-000.example.com" || last.Target != "target-024.example.com" {
		t.Fatalf("unexpected task order: first=%q last=%q", first.Target, last.Target)
	}
}

func TestEnqueueTasksCustomBatchSize(t *testing.T) {
	queue := &enqueueRecordingQueue{}
	result, err := EnqueueTasks(context.Background(), queue, "queue-url", makeEnqueueTasks(5), EnqueueOptions{
		BatchSize:   2,
		Concurrency: 1,
	})
	if err != nil {
		t.Fatalf("enqueue tasks: %v", err)
	}
	if result.BatchCount != 3 || result.SentTasks != 5 {
		t.Fatalf("result = %#v, want 3 batches and 5 sent tasks", result)
	}
	for i, want := range []int{2, 2, 1} {
		if got := len(queue.batches[i]); got != want {
			t.Fatalf("batch %d size = %d, want %d", i, got, want)
		}
	}
}

func TestEnqueueTasksUsesBoundedDefaultConcurrency(t *testing.T) {
	release := make(chan struct{})
	queue := &enqueueRecordingQueue{
		started: make(chan struct{}, 20),
		release: release,
	}
	done := make(chan struct {
		result EnqueueResult
		err    error
	}, 1)

	go func() {
		result, err := EnqueueTasks(context.Background(), queue, "queue-url", makeEnqueueTasks(20), EnqueueOptions{
			BatchSize: 1,
		})
		done <- struct {
			result EnqueueResult
			err    error
		}{result: result, err: err}
	}()

	for i := 0; i < DefaultEnqueueWorkers; i++ {
		select {
		case <-queue.started:
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for batch %d to start", i)
		}
	}

	queue.mu.Lock()
	active := queue.active
	maxActive := queue.maxActive
	queue.mu.Unlock()
	if active != DefaultEnqueueWorkers {
		t.Fatalf("active sends = %d, want %d", active, DefaultEnqueueWorkers)
	}
	if maxActive > DefaultEnqueueWorkers {
		t.Fatalf("max active sends = %d, want <= %d", maxActive, DefaultEnqueueWorkers)
	}

	select {
	case <-queue.started:
		t.Fatalf("started more than %d sends before release", DefaultEnqueueWorkers)
	case <-time.After(20 * time.Millisecond):
	}

	close(release)
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("enqueue tasks: %v", got.err)
		}
		if got.result.SentTasks != 20 || got.result.BatchCount != 20 {
			t.Fatalf("result = %#v, want 20 sent tasks and batches", got.result)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for enqueue to finish")
	}
}

func TestEnqueueTasksSendFailureIncludesBatchOffset(t *testing.T) {
	sendErr := errors.New("send failed")
	queue := &enqueueRecordingQueue{
		failCall: 1,
		sendErr:  sendErr,
	}

	result, err := EnqueueTasks(context.Background(), queue, "queue-url", makeEnqueueTasks(4), EnqueueOptions{
		BatchSize:   2,
		Concurrency: 1,
	})
	if !errors.Is(err, sendErr) {
		t.Fatalf("error = %v, want wrapping sendErr", err)
	}
	if result.SentTasks != 2 || result.BatchCount != 1 {
		t.Fatalf("result = %#v, want one successful batch before failure", result)
	}
	if !strings.Contains(err.Error(), "batch 1") || !strings.Contains(err.Error(), "tasks 2-3") {
		t.Fatalf("error should include batch and task offsets, got %v", err)
	}
}

func TestEnqueueTasksRejectsOversizedPayload(t *testing.T) {
	queue := &enqueueRecordingQueue{}
	tasks := []worker.Task{{
		ToolName: "httpx",
		Target:   strings.Repeat("x", 80),
	}}

	result, err := EnqueueTasks(context.Background(), queue, "queue-url", tasks, EnqueueOptions{
		MaxMessageBytes: 20,
	})
	if err == nil {
		t.Fatal("expected oversized payload error")
	}
	if result.SentTasks != 0 || result.BatchCount != 0 {
		t.Fatalf("result = %#v, want no sent tasks", result)
	}
	if len(queue.batches) != 0 {
		t.Fatalf("queue batches = %d, want 0", len(queue.batches))
	}
	if !strings.Contains(err.Error(), "task 0") || !strings.Contains(err.Error(), "exceeds max queue message size") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnqueueTasksMarshalFailureIncludesTaskIndex(t *testing.T) {
	marshalErr := errors.New("marshal failed")
	queue := &enqueueRecordingQueue{}
	tasks := []worker.Task{
		{ToolName: "httpx", Target: "ok"},
		{ToolName: "httpx", Target: "bad"},
	}
	marshal := func(task worker.Task) ([]byte, error) {
		if task.Target == "bad" {
			return nil, marshalErr
		}
		return json.Marshal(task)
	}

	result, err := enqueueTasks(context.Background(), queue, "queue-url", tasks, EnqueueOptions{
		BatchSize:   2,
		Concurrency: 1,
	}, marshal)
	if !errors.Is(err, marshalErr) {
		t.Fatalf("error = %v, want wrapping marshalErr", err)
	}
	if result.SentTasks != 0 || result.BatchCount != 0 {
		t.Fatalf("result = %#v, want no sent tasks", result)
	}
	if len(queue.batches) != 0 {
		t.Fatalf("queue batches = %d, want 0", len(queue.batches))
	}
	if !strings.Contains(err.Error(), "task 1") {
		t.Fatalf("error should include task index, got %v", err)
	}
}

func TestEnqueueTasksContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	queue := &enqueueRecordingQueue{}
	result, err := EnqueueTasks(ctx, queue, "queue-url", makeEnqueueTasks(1), EnqueueOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if result.SentTasks != 0 || result.BatchCount != 0 {
		t.Fatalf("result = %#v, want no sent tasks", result)
	}
	if len(queue.batches) != 0 {
		t.Fatalf("queue batches = %d, want 0", len(queue.batches))
	}
}
