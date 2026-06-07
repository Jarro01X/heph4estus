package targetlist

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	DefaultTargetChunkSize int64 = 16 * 1024 * 1024
	MaxSafeChunkSize       int64 = 64 * 1024 * 1024
	ScannerMaxTokenSize          = 16 * 1024 * 1024
)

// Policy controls target-list chunk planning.
type Policy struct {
	RequestedChunks     int
	WorkerCount         int
	TargetChunkSize     int64
	MaxChunkSize        int64
	ScannerMaxTokenSize int
}

// Metadata is the bounded-memory preflight result for a target-list file.
type Metadata struct {
	Path             string
	TotalTargets     int
	TotalSourceBytes int64
	EffectiveChunks  int
	RequestedChunks  int
	TargetChunkSize  int64
	MaxChunkSize     int64
	totalEntryBytes  int64
}

// Chunk describes one temporary target-list chunk file and its final upload key.
type Chunk struct {
	Path        string
	Key         string
	ByteSize    int64
	TargetCount int
	Index       int
	TotalChunks int
}

// Result contains the streaming split result and per-chunk metadata.
type Result struct {
	Metadata
	Chunks []Chunk
}

// Cleanup removes all temporary chunk files produced by SplitFile.
func (r *Result) Cleanup() error {
	if r == nil {
		return nil
	}
	var errs []error
	for _, chunk := range r.Chunks {
		if chunk.Path == "" {
			continue
		}
		if err := os.Remove(chunk.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// InspectFile validates a target-list path and returns lightweight metadata
// without keeping file contents in memory.
func InspectFile(path string, policy Policy) (*Metadata, error) {
	policy = policy.withDefaults()
	if policy.RequestedChunks < 0 {
		return nil, fmt.Errorf("requested chunk count must be positive")
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat target file: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("target file path %q is a directory", path)
	}

	sourceBytes := info.Size()
	if policy.RequestedChunks > 0 {
		avg := ceilDiv(sourceBytes, int64(policy.RequestedChunks))
		if avg > policy.MaxChunkSize {
			return nil, fmt.Errorf("requested chunk count %d would average %s per chunk, above max safe chunk size %s", policy.RequestedChunks, formatBytes(avg), formatBytes(policy.MaxChunkSize))
		}
	}

	targets, entryBytes, err := scanTargetStats(path, policy.ScannerMaxTokenSize)
	if err != nil {
		return nil, err
	}
	if targets == 0 {
		return nil, fmt.Errorf("no targets found in %s", path)
	}

	effective := effectiveChunkCount(sourceBytes, targets, policy)
	return &Metadata{
		Path:             path,
		TotalTargets:     targets,
		TotalSourceBytes: sourceBytes,
		EffectiveChunks:  effective,
		RequestedChunks:  policy.RequestedChunks,
		TargetChunkSize:  policy.TargetChunkSize,
		MaxChunkSize:     policy.MaxChunkSize,
		totalEntryBytes:  entryBytes,
	}, nil
}

// SplitFile streams path into temporary target-list chunk files under tempDir.
// Targets are normalized with the same trim/comment rules as generic scans.
func SplitFile(path, tempDir string, policy Policy, keyForChunk func(int) string) (*Result, error) {
	meta, err := InspectFile(path, policy)
	if err != nil {
		return nil, err
	}
	result, err := splitWithMetadata(meta, tempDir, policy.withDefaults(), keyForChunk)
	if err != nil {
		if result != nil {
			_ = result.Cleanup()
		}
		return nil, err
	}
	return result, nil
}

// StreamTargets calls fn for each effective target in path without retaining
// the target list in memory.
func StreamTargets(path string, scannerMax int, fn func(index int, target string) error) (int, error) {
	if scannerMax <= 0 {
		scannerMax = ScannerMaxTokenSize
	}
	file, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("opening target file: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	scanner := newScanner(file, scannerMax)
	var count int
	for scanner.Scan() {
		target, ok := normalizeTargetLine(scanner.Text())
		if !ok {
			continue
		}
		if err := fn(count, target); err != nil {
			return count, err
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		return count, scannerError(err, scannerMax)
	}
	return count, nil
}

func splitWithMetadata(meta *Metadata, tempDir string, policy Policy, keyForChunk func(int) string) (*Result, error) {
	if tempDir == "" {
		tempDir = os.TempDir()
	}
	if err := os.MkdirAll(tempDir, 0o700); err != nil {
		return nil, fmt.Errorf("creating target-list temp dir: %w", err)
	}

	file, err := os.Open(meta.Path)
	if err != nil {
		return nil, fmt.Errorf("opening target file: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	result := &Result{Metadata: *meta}
	targetBytes := ceilDiv(meta.totalEntryBytes, int64(meta.EffectiveChunks))
	if targetBytes < 1 {
		targetBytes = 1
	}

	scanner := newScanner(file, policy.ScannerMaxTokenSize)
	var (
		currentFile  *os.File
		writer       *bufio.Writer
		chunkIndex   int
		chunkBytes   int64
		chunkTargets int
		processed    int
		currentPath  string
	)

	cleanupOpenChunk := func() {
		if currentFile != nil {
			_ = currentFile.Close()
			currentFile = nil
		}
		writer = nil
		if currentPath != "" {
			_ = os.Remove(currentPath)
			currentPath = ""
		}
		chunkBytes = 0
		chunkTargets = 0
	}

	cleanupFailedSplit := func() {
		cleanupOpenChunk()
		_ = result.Cleanup()
	}

	startChunk := func() error {
		f, err := os.CreateTemp(tempDir, fmt.Sprintf("chunk_%06d_*.txt", chunkIndex))
		if err != nil {
			return fmt.Errorf("creating target-list chunk %d: %w", chunkIndex, err)
		}
		currentFile = f
		writer = bufio.NewWriter(f)
		currentPath = f.Name()
		chunkBytes = 0
		chunkTargets = 0
		return nil
	}

	finishChunk := func() error {
		if currentFile == nil {
			return nil
		}
		if err := writer.Flush(); err != nil {
			cleanupOpenChunk()
			return fmt.Errorf("flushing target-list chunk %d: %w", chunkIndex, err)
		}
		if err := currentFile.Close(); err != nil {
			cleanupOpenChunk()
			return fmt.Errorf("closing target-list chunk %d: %w", chunkIndex, err)
		}
		key := ""
		if keyForChunk != nil {
			key = keyForChunk(chunkIndex)
		}
		result.Chunks = append(result.Chunks, Chunk{
			Path:        currentPath,
			Key:         key,
			ByteSize:    chunkBytes,
			TargetCount: chunkTargets,
			Index:       chunkIndex,
			TotalChunks: meta.EffectiveChunks,
		})
		currentFile = nil
		writer = nil
		currentPath = ""
		return nil
	}

	for scanner.Scan() {
		target, ok := normalizeTargetLine(scanner.Text())
		if !ok {
			continue
		}

		entryBytes := int64(len(target) + 1)
		if entryBytes > policy.MaxChunkSize {
			cleanupFailedSplit()
			return result, fmt.Errorf("target entry is %s, above max safe chunk size %s", formatBytes(entryBytes), formatBytes(policy.MaxChunkSize))
		}

		futureChunks := meta.EffectiveChunks - chunkIndex - 1
		if currentFile != nil && chunkTargets > 0 && chunkBytes+entryBytes > policy.MaxChunkSize {
			if futureChunks <= 0 {
				cleanupFailedSplit()
				return result, fmt.Errorf("target-list chunk %d would exceed max safe chunk size %s", chunkIndex, formatBytes(policy.MaxChunkSize))
			}
			if err := finishChunk(); err != nil {
				cleanupFailedSplit()
				return result, err
			}
			chunkIndex++
			futureChunks = meta.EffectiveChunks - chunkIndex - 1
		}

		remainingTargetsIncludingLine := meta.TotalTargets - processed
		sizeSplit := currentFile != nil && chunkTargets > 0 && chunkBytes+entryBytes > targetBytes && futureChunks > 0
		forceSplit := currentFile != nil && chunkTargets > 0 && futureChunks > 0 && remainingTargetsIncludingLine <= futureChunks
		if sizeSplit || forceSplit {
			if err := finishChunk(); err != nil {
				cleanupFailedSplit()
				return result, err
			}
			chunkIndex++
		}

		if currentFile == nil {
			if err := startChunk(); err != nil {
				cleanupFailedSplit()
				return result, err
			}
		}
		if _, err := writer.WriteString(target); err != nil {
			cleanupFailedSplit()
			return result, fmt.Errorf("writing target-list chunk %d: %w", chunkIndex, err)
		}
		if err := writer.WriteByte('\n'); err != nil {
			cleanupFailedSplit()
			return result, fmt.Errorf("writing target-list chunk %d: %w", chunkIndex, err)
		}
		chunkBytes += entryBytes
		chunkTargets++
		processed++
	}
	if err := scanner.Err(); err != nil {
		cleanupFailedSplit()
		return result, scannerError(err, policy.ScannerMaxTokenSize)
	}
	if err := finishChunk(); err != nil {
		cleanupFailedSplit()
		return result, err
	}

	result.EffectiveChunks = len(result.Chunks)
	for i := range result.Chunks {
		result.Chunks[i].TotalChunks = result.EffectiveChunks
	}
	return result, nil
}

func scanTargetStats(path string, scannerMax int) (int, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0, fmt.Errorf("opening target file: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	scanner := newScanner(file, scannerMax)
	var targets int
	var entryBytes int64
	for scanner.Scan() {
		target, ok := normalizeTargetLine(scanner.Text())
		if !ok {
			continue
		}
		targets++
		entryBytes += int64(len(target) + 1)
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, scannerError(err, scannerMax)
	}
	return targets, entryBytes, nil
}

func normalizeTargetLine(line string) (string, bool) {
	target := strings.TrimSpace(line)
	if target == "" || strings.HasPrefix(target, "#") {
		return "", false
	}
	return target, true
}

func newScanner(file *os.File, maxTokenSize int) *bufio.Scanner {
	scanner := bufio.NewScanner(file)
	initialSize := 64 * 1024
	if maxTokenSize < initialSize {
		initialSize = maxTokenSize
	}
	if initialSize < 1 {
		initialSize = 1
	}
	scanner.Buffer(make([]byte, initialSize), maxTokenSize)
	return scanner
}

func scannerError(err error, maxTokenSize int) error {
	if strings.Contains(err.Error(), "token too long") {
		return fmt.Errorf("target line exceeds scanner max token size %s", formatBytes(int64(maxTokenSize)))
	}
	return fmt.Errorf("scanning target file: %w", err)
}

func effectiveChunkCount(sourceBytes int64, totalTargets int, policy Policy) int {
	var desired int
	if policy.RequestedChunks > 0 {
		desired = policy.RequestedChunks
	} else {
		desired = int(ceilDiv(sourceBytes, policy.TargetChunkSize))
		if desired < policy.WorkerCount {
			desired = policy.WorkerCount
		}
	}
	if desired < 1 {
		desired = 1
	}
	if totalTargets > 0 && desired > totalTargets {
		desired = totalTargets
	}
	return desired
}

func (p Policy) withDefaults() Policy {
	if p.TargetChunkSize <= 0 {
		p.TargetChunkSize = DefaultTargetChunkSize
	}
	if p.MaxChunkSize <= 0 {
		p.MaxChunkSize = MaxSafeChunkSize
	}
	if p.ScannerMaxTokenSize <= 0 {
		p.ScannerMaxTokenSize = ScannerMaxTokenSize
	}
	return p
}

func ceilDiv(n, d int64) int64 {
	if d <= 0 {
		return 0
	}
	return (n + d - 1) / d
}

func formatBytes(n int64) string {
	const mib = 1024 * 1024
	if n%mib == 0 && n >= mib {
		return fmt.Sprintf("%d MiB", n/mib)
	}
	return fmt.Sprintf("%d bytes", n)
}
