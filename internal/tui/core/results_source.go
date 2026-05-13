package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"heph4estus/internal/cloud"
)

// ResultsSource abstracts how results are listed and downloaded.
// Implementations exist for S3-backed and local-filesystem-backed results.
type ResultsSource interface {
	// ListKeys returns all result keys. Keys are opaque identifiers that
	// can be passed to Download and to jobs.TargetFromKey.
	ListKeys(ctx context.Context) ([]string, error)
	// Download returns the raw bytes for the given result key.
	Download(ctx context.Context, key string) ([]byte, error)
}

// ArtifactSource reads tool output artifacts referenced by worker.Result.
type ArtifactSource interface {
	DownloadArtifact(ctx context.Context, outputKey string) ([]byte, error)
}

// S3ResultsSource reads results from an S3 bucket via cloud.Storage.
type S3ResultsSource struct {
	Storage cloud.Storage
	Bucket  string
	Prefix  string
}

func (s *S3ResultsSource) ListKeys(ctx context.Context) ([]string, error) {
	return s.Storage.List(ctx, s.Bucket, s.Prefix)
}

func (s *S3ResultsSource) Download(ctx context.Context, key string) ([]byte, error) {
	return s.Storage.Download(ctx, s.Bucket, key)
}

func (s *S3ResultsSource) DownloadArtifact(ctx context.Context, outputKey string) ([]byte, error) {
	return s.Storage.Download(ctx, s.Bucket, s3ObjectKey(outputKey))
}

// LocalResultsSource reads results from a local export directory.
// Keys are relative paths within the results directory, matching the
// suffix format used in S3 keys (e.g. "target_123.json").
type LocalResultsSource struct {
	ResultsDir   string // e.g. <outDir>/<tool>/<jobID>/results
	ArtifactsDir string // e.g. <outDir>/<tool>/<jobID>/artifacts
}

func (l *LocalResultsSource) ListKeys(_ context.Context) ([]string, error) {
	if _, err := os.Stat(l.ResultsDir); os.IsNotExist(err) {
		return nil, nil
	}

	var keys []string
	err := filepath.Walk(l.ResultsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(l.ResultsDir, path)
		if err != nil {
			return err
		}
		// Use forward slashes for consistency with S3 key format.
		keys = append(keys, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking results dir: %w", err)
	}
	return keys, nil
}

func (l *LocalResultsSource) Download(_ context.Context, key string) ([]byte, error) {
	// Convert forward-slash key back to OS path.
	localPath := filepath.Join(l.ResultsDir, filepath.FromSlash(strings.TrimPrefix(key, "/")))
	data, err := os.ReadFile(localPath)
	if err != nil {
		return nil, fmt.Errorf("reading local result %s: %w", key, err)
	}
	return data, nil
}

func (l *LocalResultsSource) DownloadArtifact(_ context.Context, outputKey string) ([]byte, error) {
	artifactsDir := l.ArtifactsDir
	if artifactsDir == "" {
		artifactsDir = filepath.Join(filepath.Dir(l.ResultsDir), "artifacts")
	}
	rel := artifactRelativePath(outputKey)
	localPath := filepath.Join(artifactsDir, filepath.FromSlash(rel))
	data, err := os.ReadFile(localPath)
	if err != nil {
		return nil, fmt.Errorf("reading local artifact %s: %w", outputKey, err)
	}
	return data, nil
}

func s3ObjectKey(key string) string {
	key = strings.TrimSpace(key)
	if strings.HasPrefix(key, "s3://") {
		withoutScheme := strings.TrimPrefix(key, "s3://")
		if idx := strings.Index(withoutScheme, "/"); idx >= 0 {
			return withoutScheme[idx+1:]
		}
	}
	return strings.TrimPrefix(key, "/")
}

func artifactRelativePath(key string) string {
	key = s3ObjectKey(key)
	if idx := strings.Index(key, "/artifacts/"); idx >= 0 {
		return key[idx+len("/artifacts/"):]
	}
	key = strings.TrimPrefix(key, "artifacts/")
	return strings.TrimPrefix(key, "/")
}

// Destroyer abstracts infrastructure teardown for the results view.
type Destroyer interface {
	Destroy(ctx context.Context) error
}

// TerraformDestroyer runs terraform destroy against a working directory.
type TerraformDestroyer struct {
	DestroyFunc func(ctx context.Context, workDir string) error
	WorkDir     string
}

func (t *TerraformDestroyer) Destroy(ctx context.Context) error {
	return t.DestroyFunc(ctx, t.WorkDir)
}
