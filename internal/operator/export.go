package operator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"heph4estus/internal/cloud"
	"heph4estus/internal/jobs"
)

// ExportResult summarises what was written to the local output directory.
type ExportResult struct {
	Dir            string // root output dir: <out>/<tool>/<job_id>
	ResultCount    int
	ArtifactCount  int
}

// ExportJob downloads results and artifacts from S3 to a predictable local
// layout:
//
//	<outDir>/<tool>/<jobID>/results/...
//	<outDir>/<tool>/<jobID>/artifacts/...
//
// It returns the counts of files written so callers can report progress.
// Any download failure is returned immediately — partial exports are not
// silently swallowed.
func ExportJob(ctx context.Context, storage cloud.Storage, bucket, tool, jobID, outDir string) (*ExportResult, error) {
	jobDir := filepath.Join(outDir, tool, jobID)

	resultPrefix := jobs.ResultPrefix(tool, jobID)
	artifactPrefix := jobs.ArtifactPrefix(tool, jobID)

	resultCount, err := downloadPrefix(ctx, storage, bucket, resultPrefix, filepath.Join(jobDir, "results"))
	if err != nil {
		return nil, fmt.Errorf("exporting results: %w", err)
	}

	artifactCount, err := downloadPrefix(ctx, storage, bucket, artifactPrefix, filepath.Join(jobDir, "artifacts"))
	if err != nil {
		return nil, fmt.Errorf("exporting artifacts: %w", err)
	}

	return &ExportResult{
		Dir:           jobDir,
		ResultCount:   resultCount,
		ArtifactCount: artifactCount,
	}, nil
}

// downloadPrefix lists all keys under prefix and writes each object to the
// corresponding local path, preserving the suffix after the prefix as the
// relative file path.
func downloadPrefix(ctx context.Context, storage cloud.Storage, bucket, prefix, localDir string) (int, error) {
	keys, err := storage.List(ctx, bucket, prefix)
	if err != nil {
		return 0, fmt.Errorf("listing %s: %w", prefix, err)
	}
	if len(keys) == 0 {
		return 0, nil
	}

	count := 0
	for _, key := range keys {
		rel := strings.TrimPrefix(key, prefix)
		if rel == "" || rel == key {
			continue
		}

		data, err := storage.Download(ctx, bucket, key)
		if err != nil {
			return count, fmt.Errorf("downloading %s: %w", key, err)
		}

		dest := filepath.Join(localDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return count, fmt.Errorf("creating directory for %s: %w", dest, err)
		}
		if err := os.WriteFile(dest, data, 0o644); err != nil {
			return count, fmt.Errorf("writing %s: %w", dest, err)
		}
		count++
	}
	return count, nil
}
