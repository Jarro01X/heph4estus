package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"heph4estus/internal/cloud"
	"heph4estus/internal/worker"
)

const (
	// DefaultEnqueueBatchSize matches the SQS SendMessageBatch limit.
	DefaultEnqueueBatchSize = 10
	DefaultEnqueueWorkers   = 4
	// DefaultMaxQueueMessageBytes matches the SQS message body limit.
	DefaultMaxQueueMessageBytes = 256 * 1024
)

// EnqueueOptions controls how worker tasks are marshaled and sent to a queue.
type EnqueueOptions struct {
	BatchSize       int
	Concurrency     int
	MaxMessageBytes int
}

// EnqueueResult reports how much work was successfully published.
type EnqueueResult struct {
	TotalTasks int
	SentTasks  int
	// BatchCount is the number of batches successfully accepted by the queue.
	BatchCount int
	Elapsed    time.Duration
}

type enqueueBatch struct {
	index int
	start int
	end   int
}

type enqueueBatchResult struct {
	sent int
	err  error
}

type taskMarshaler func(worker.Task) ([]byte, error)

// EnqueueTasks sends tasks through a bounded worker pool without materializing
// every JSON message body in memory at once.
func EnqueueTasks(ctx context.Context, queue cloud.Queue, queueID string, tasks []worker.Task, opts EnqueueOptions) (EnqueueResult, error) {
	return enqueueTasks(ctx, queue, queueID, tasks, opts, marshalWorkerTask)
}

func enqueueTasks(ctx context.Context, queue cloud.Queue, queueID string, tasks []worker.Task, opts EnqueueOptions, marshal taskMarshaler) (result EnqueueResult, err error) {
	started := time.Now()
	result.TotalTasks = len(tasks)
	defer func() {
		result.Elapsed = time.Since(started)
	}()

	if len(tasks) == 0 {
		return result, nil
	}
	if queue == nil {
		return result, fmt.Errorf("enqueue queue is nil")
	}
	if marshal == nil {
		return result, fmt.Errorf("enqueue task marshaler is nil")
	}

	opts = normalizeEnqueueOptions(opts)
	batchCount := (len(tasks) + opts.BatchSize - 1) / opts.BatchSize
	workerCount := opts.Concurrency
	if workerCount > batchCount {
		workerCount = batchCount
	}

	enqueueCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan enqueueBatch)
	results := make(chan enqueueBatchResult)

	var firstErrMu sync.Mutex
	var firstErr error
	recordErr := func(err error) {
		if err == nil {
			return
		}
		firstErrMu.Lock()
		defer firstErrMu.Unlock()
		if firstErr == nil {
			firstErr = err
			cancel()
		}
	}

	var wg sync.WaitGroup
	wg.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go func() {
			defer wg.Done()
			for batch := range jobs {
				sent, sendErr := sendTaskBatch(enqueueCtx, queue, queueID, tasks, batch, opts.MaxMessageBytes, marshal)
				recordErr(sendErr)
				results <- enqueueBatchResult{sent: sent, err: sendErr}
			}
		}()
	}

	go func() {
		defer close(jobs)
		for start := 0; start < len(tasks); start += opts.BatchSize {
			end := start + opts.BatchSize
			if end > len(tasks) {
				end = len(tasks)
			}
			batch := enqueueBatch{
				index: start / opts.BatchSize,
				start: start,
				end:   end,
			}
			select {
			case <-enqueueCtx.Done():
				return
			case jobs <- batch:
			}
		}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	for batchResult := range results {
		if batchResult.err == nil {
			result.SentTasks += batchResult.sent
			result.BatchCount++
		}
	}
	firstErrMu.Lock()
	err = firstErr
	firstErrMu.Unlock()
	if err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	return result, nil
}

func normalizeEnqueueOptions(opts EnqueueOptions) EnqueueOptions {
	if opts.BatchSize <= 0 {
		opts.BatchSize = DefaultEnqueueBatchSize
	}
	if opts.BatchSize > DefaultEnqueueBatchSize {
		opts.BatchSize = DefaultEnqueueBatchSize
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = DefaultEnqueueWorkers
	}
	if opts.MaxMessageBytes <= 0 {
		opts.MaxMessageBytes = DefaultMaxQueueMessageBytes
	}
	return opts
}

func sendTaskBatch(ctx context.Context, queue cloud.Queue, queueID string, tasks []worker.Task, batch enqueueBatch, maxMessageBytes int, marshal taskMarshaler) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	bodies := make([]string, 0, batch.end-batch.start)
	for i := batch.start; i < batch.end; i++ {
		body, err := marshal(tasks[i])
		if err != nil {
			return 0, fmt.Errorf("marshal task %d in enqueue batch %d: %w", i, batch.index, err)
		}
		if len(body) > maxMessageBytes {
			return 0, fmt.Errorf("task %d in enqueue batch %d exceeds max queue message size (%d > %d bytes)", i, batch.index, len(body), maxMessageBytes)
		}
		bodies = append(bodies, string(body))
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := queue.SendBatch(ctx, queueID, bodies); err != nil {
		return 0, fmt.Errorf("send enqueue batch %d (tasks %d-%d): %w", batch.index, batch.start, batch.end-1, err)
	}
	return len(bodies), nil
}

func marshalWorkerTask(task worker.Task) ([]byte, error) {
	return json.Marshal(task)
}
