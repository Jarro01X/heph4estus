package jobs

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"heph4estus/internal/cloud"
	wordlisttool "heph4estus/internal/tools/wordlist"
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
	Tasks []worker.Task

	// ChunkData is used by the legacy in-memory planner.
	ChunkData [][]byte
	// ChunkFiles is used by the streaming file-based planner.
	ChunkFiles []WordlistChunk
	ChunkKeys  []string

	TotalWords       int
	TotalSourceBytes int64
	EffectiveChunks  int
	RequestedChunks  int
	TargetChunkSize  int64
	MaxChunkSize     int64

	cleanup func() error
}

// WordlistChunk describes a temporary chunk file prepared for upload.
type WordlistChunk struct {
	Path        string
	Key         string
	ByteSize    int64
	WordCount   int
	Index       int
	TotalChunks int
}

// Cleanup removes temporary chunk files for file-based plans.
func (p *WordlistPlan) Cleanup() error {
	if p == nil || p.cleanup == nil {
		return nil
	}
	cleanup := p.cleanup
	p.cleanup = nil
	return cleanup()
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
		Tasks:           make([]worker.Task, len(chunks)),
		ChunkData:       make([][]byte, len(chunks)),
		ChunkKeys:       make([]string, len(chunks)),
		TotalWords:      len(entries),
		EffectiveChunks: len(chunks),
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

// PlanWordlistFile splits a wordlist file into temporary chunk files and prepares tasks.
func PlanWordlistFile(toolName, jobID, runtimeTarget, options, wordlistPath, tempDir string, chunkCount, workerCount int) (*WordlistPlan, error) {
	result, err := wordlisttool.SplitFile(wordlistPath, tempDir, wordlisttool.Policy{
		RequestedChunks: chunkCount,
		WorkerCount:     workerCount,
	}, func(i int) string {
		return InputKey(toolName, jobID, i)
	})
	if err != nil {
		return nil, err
	}

	groupID := SafeTargetStem(runtimeTarget)
	plan := &WordlistPlan{
		Tasks:            make([]worker.Task, len(result.Chunks)),
		ChunkFiles:       make([]WordlistChunk, len(result.Chunks)),
		ChunkKeys:        make([]string, len(result.Chunks)),
		TotalWords:       result.TotalWords,
		TotalSourceBytes: result.TotalSourceBytes,
		EffectiveChunks:  result.EffectiveChunks,
		RequestedChunks:  result.RequestedChunks,
		TargetChunkSize:  result.TargetChunkSize,
		MaxChunkSize:     result.MaxChunkSize,
		cleanup:          result.Cleanup,
	}

	for i, chunk := range result.Chunks {
		plan.ChunkKeys[i] = chunk.Key
		plan.ChunkFiles[i] = WordlistChunk{
			Path:        chunk.Path,
			Key:         chunk.Key,
			ByteSize:    chunk.ByteSize,
			WordCount:   chunk.WordCount,
			Index:       chunk.Index,
			TotalChunks: chunk.TotalChunks,
		}
		plan.Tasks[i] = worker.Task{
			ToolName:    toolName,
			JobID:       jobID,
			Target:      runtimeTarget,
			InputKey:    chunk.Key,
			Options:     options,
			GroupID:     groupID,
			ChunkIdx:    chunk.Index,
			TotalChunks: chunk.TotalChunks,
		}
	}

	return plan, nil
}

// UploadChunks uploads all chunk files to storage.
func UploadChunks(ctx context.Context, storage cloud.Storage, bucket string, plan *WordlistPlan) error {
	if len(plan.ChunkFiles) > 0 {
		for _, chunk := range plan.ChunkFiles {
			if chunk.ByteSize > plan.MaxChunkSize && plan.MaxChunkSize > 0 {
				return fmt.Errorf("chunk %d (%s) is %d bytes, above max safe chunk size %d", chunk.Index, chunk.Key, chunk.ByteSize, plan.MaxChunkSize)
			}
			data, err := os.ReadFile(chunk.Path)
			if err != nil {
				return fmt.Errorf("reading chunk %d (%s): %w", chunk.Index, chunk.Key, err)
			}
			if int64(len(data)) > plan.MaxChunkSize && plan.MaxChunkSize > 0 {
				return fmt.Errorf("chunk %d (%s) is %d bytes, above max safe chunk size %d", chunk.Index, chunk.Key, len(data), plan.MaxChunkSize)
			}
			if err := storage.Upload(ctx, bucket, chunk.Key, data); err != nil {
				return fmt.Errorf("uploading chunk %d (%s): %w", chunk.Index, chunk.Key, err)
			}
		}
		return nil
	}

	for i, data := range plan.ChunkData {
		if err := storage.Upload(ctx, bucket, plan.ChunkKeys[i], data); err != nil {
			return fmt.Errorf("uploading chunk %d (%s): %w", i, plan.ChunkKeys[i], err)
		}
	}
	return nil
}
