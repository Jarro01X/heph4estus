package jobs

import (
	"bufio"
	"context"
	"fmt"
	"strings"

	"heph4estus/internal/cloud"
	"heph4estus/internal/worker"
)

// ParseWordlistEntries splits raw wordlist content into non-empty lines while
// preserving leading/trailing spaces and '#' prefixes exactly as provided.
func ParseWordlistEntries(content string) []string {
	var entries []string
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		entries = append(entries, line)
	}
	return entries
}

// ChunkEntries splits entries into n non-empty chunks with deterministic ordering.
// The effective chunk count is clamped to len(entries) so no empty chunks are produced.
func ChunkEntries(entries []string, n int) [][]string {
	if n <= 0 {
		n = 1
	}
	if n > len(entries) {
		n = len(entries)
	}
	chunks := make([][]string, n)
	for i, e := range entries {
		chunks[i%n] = append(chunks[i%n], e)
	}
	return chunks
}

// WordlistPlan holds the prepared chunk tasks and their upload keys.
type WordlistPlan struct {
	Tasks      []worker.Task
	ChunkData  [][]byte  // raw bytes to upload per chunk
	ChunkKeys  []string  // S3 keys for each chunk
	TotalWords int
}

// PlanWordlistJob splits a wordlist into chunks and prepares tasks.
func PlanWordlistJob(toolName, jobID, runtimeTarget, options string, wordlistContent string, chunkCount int) (*WordlistPlan, error) {
	entries := ParseWordlistEntries(wordlistContent)
	if len(entries) == 0 {
		return nil, fmt.Errorf("no entries found in wordlist")
	}

	chunks := ChunkEntries(entries, chunkCount)
	groupID := SafeTargetStem(runtimeTarget)

	plan := &WordlistPlan{
		Tasks:      make([]worker.Task, len(chunks)),
		ChunkData:  make([][]byte, len(chunks)),
		ChunkKeys:  make([]string, len(chunks)),
		TotalWords: len(entries),
	}

	for i, chunk := range chunks {
		key := InputKey(toolName, jobID, i)
		plan.ChunkKeys[i] = key
		plan.ChunkData[i] = []byte(strings.Join(chunk, "\n") + "\n")
		plan.Tasks[i] = worker.Task{
			ToolName:    toolName,
			JobID:       jobID,
			Target:      runtimeTarget,
			InputKey:    key,
			Options:     options,
			GroupID:     groupID,
			ChunkIdx:    i,
			TotalChunks: len(chunks),
		}
	}

	return plan, nil
}

// UploadChunks uploads all chunk files to storage.
func UploadChunks(ctx context.Context, storage cloud.Storage, bucket string, plan *WordlistPlan) error {
	for i, data := range plan.ChunkData {
		if err := storage.Upload(ctx, bucket, plan.ChunkKeys[i], data); err != nil {
			return fmt.Errorf("uploading chunk %d: %w", i, err)
		}
	}
	return nil
}
