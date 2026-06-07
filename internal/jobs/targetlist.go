package jobs

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"heph4estus/internal/cloud"
	targetlisttool "heph4estus/internal/tools/targetlist"
	"heph4estus/internal/worker"
)

// TargetListPlan holds prepared target-list tasks and optional upload chunks.
type TargetListPlan struct {
	Tasks []worker.Task

	ChunkFiles []TargetListChunk
	ChunkKeys  []string

	TotalTargets     int
	TotalSourceBytes int64
	EffectiveChunks  int
	RequestedChunks  int
	TargetChunkSize  int64
	MaxChunkSize     int64
	FileBacked       bool

	cleanup func() error
}

// TargetListChunk describes a temporary target-list chunk file prepared for upload.
type TargetListChunk struct {
	Path        string
	Key         string
	ByteSize    int64
	TargetCount int
	Index       int
	TotalChunks int
}

// Cleanup removes temporary chunk files for file-backed plans.
func (p *TargetListPlan) Cleanup() error {
	if p == nil || p.cleanup == nil {
		return nil
	}
	cleanup := p.cleanup
	p.cleanup = nil
	return cleanup()
}

// PlanTargetListFile streams a target-list file and prepares either per-target
// tasks or file-backed chunk tasks, depending on fileBacked.
func PlanTargetListFile(toolName, jobID, options, targetPath, tempDir string, workerCount int, fileBacked bool) (*TargetListPlan, error) {
	if fileBacked {
		return planTargetListChunks(toolName, jobID, options, targetPath, tempDir, workerCount)
	}
	return planTargetListTargets(toolName, jobID, options, targetPath, workerCount)
}

// PlanTargetListContent preserves the legacy in-memory test path.
func PlanTargetListContent(toolName, jobID, options, content string) (*TargetListPlan, error) {
	targets := ParseTargetEntries(content)
	if len(targets) == 0 {
		return nil, fmt.Errorf("no targets found")
	}
	plan := &TargetListPlan{
		Tasks:           make([]worker.Task, len(targets)),
		TotalTargets:    len(targets),
		EffectiveChunks: len(targets),
	}
	for i, target := range targets {
		plan.Tasks[i] = worker.Task{
			ToolName: toolName,
			JobID:    jobID,
			Target:   target,
			Options:  options,
		}
	}
	return plan, nil
}

func planTargetListTargets(toolName, jobID, options, targetPath string, workerCount int) (*TargetListPlan, error) {
	meta, err := targetlisttool.InspectFile(targetPath, targetlisttool.Policy{WorkerCount: workerCount})
	if err != nil {
		return nil, err
	}
	plan := &TargetListPlan{
		Tasks:            make([]worker.Task, 0, meta.TotalTargets),
		TotalTargets:     meta.TotalTargets,
		TotalSourceBytes: meta.TotalSourceBytes,
		EffectiveChunks:  meta.TotalTargets,
		RequestedChunks:  meta.RequestedChunks,
		TargetChunkSize:  meta.TargetChunkSize,
		MaxChunkSize:     meta.MaxChunkSize,
	}
	_, err = targetlisttool.StreamTargets(targetPath, targetlisttool.ScannerMaxTokenSize, func(_ int, target string) error {
		plan.Tasks = append(plan.Tasks, worker.Task{
			ToolName: toolName,
			JobID:    jobID,
			Target:   target,
			Options:  options,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return plan, nil
}

func planTargetListChunks(toolName, jobID, options, targetPath, tempDir string, workerCount int) (*TargetListPlan, error) {
	result, err := targetlisttool.SplitFile(targetPath, tempDir, targetlisttool.Policy{
		WorkerCount: workerCount,
	}, func(i int) string {
		return InputKey(toolName, jobID, i)
	})
	if err != nil {
		return nil, err
	}

	targetLabel := filepath.Base(targetPath)
	groupID := SafeTargetStem(targetLabel)
	plan := &TargetListPlan{
		Tasks:            make([]worker.Task, len(result.Chunks)),
		ChunkFiles:       make([]TargetListChunk, len(result.Chunks)),
		ChunkKeys:        make([]string, len(result.Chunks)),
		TotalTargets:     result.TotalTargets,
		TotalSourceBytes: result.TotalSourceBytes,
		EffectiveChunks:  result.EffectiveChunks,
		RequestedChunks:  result.RequestedChunks,
		TargetChunkSize:  result.TargetChunkSize,
		MaxChunkSize:     result.MaxChunkSize,
		FileBacked:       true,
		cleanup:          result.Cleanup,
	}
	for i, chunk := range result.Chunks {
		plan.ChunkKeys[i] = chunk.Key
		plan.ChunkFiles[i] = TargetListChunk{
			Path:        chunk.Path,
			Key:         chunk.Key,
			ByteSize:    chunk.ByteSize,
			TargetCount: chunk.TargetCount,
			Index:       chunk.Index,
			TotalChunks: chunk.TotalChunks,
		}
		plan.Tasks[i] = worker.Task{
			ToolName:    toolName,
			JobID:       jobID,
			Target:      targetLabel,
			InputKey:    chunk.Key,
			Options:     options,
			GroupID:     groupID,
			ChunkIdx:    chunk.Index,
			TotalChunks: chunk.TotalChunks,
		}
	}
	return plan, nil
}

// UploadTargetListChunks uploads all target-list chunk files to storage.
func UploadTargetListChunks(ctx context.Context, storage cloud.Storage, bucket string, plan *TargetListPlan) error {
	for _, chunk := range plan.ChunkFiles {
		err := uploadFileChunk(ctx, storage, bucket, fileChunkUpload{
			Path:     chunk.Path,
			Key:      chunk.Key,
			ByteSize: chunk.ByteSize,
			Index:    chunk.Index,
		}, plan.MaxChunkSize)
		if err != nil {
			return err
		}
	}
	return nil
}

// ParseTargetEntries splits target content into non-empty, non-comment targets.
func ParseTargetEntries(content string) []string {
	var targets []string
	for _, line := range strings.Split(content, "\n") {
		target := strings.TrimSpace(line)
		if target == "" || strings.HasPrefix(target, "#") {
			continue
		}
		targets = append(targets, target)
	}
	return targets
}
