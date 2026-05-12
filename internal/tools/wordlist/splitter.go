package wordlist

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

// Policy controls wordlist chunk planning.
type Policy struct {
	RequestedChunks     int
	WorkerCount         int
	TargetChunkSize     int64
	MaxChunkSize        int64
	ScannerMaxTokenSize int
}

// Metadata is the bounded-memory preflight result for a wordlist file.
type Metadata struct {
	Path             string
	TotalWords       int
	TotalSourceBytes int64
	EffectiveChunks  int
	RequestedChunks  int
	TargetChunkSize  int64
	MaxChunkSize     int64
	totalEntryBytes  int64
}

// Chunk describes one temporary chunk file and its final upload key.
type Chunk struct {
	Path        string
	Key         string
	ByteSize    int64
	WordCount   int
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

// InspectFile validates a wordlist path and returns lightweight metadata
// without keeping file contents in memory.
func InspectFile(path string, policy Policy) (*Metadata, error) {
	policy = policy.withDefaults()
	if policy.RequestedChunks < 0 {
		return nil, fmt.Errorf("requested chunk count must be positive")
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat wordlist file: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("wordlist path %q is a directory", path)
	}

	sourceBytes := info.Size()
	if policy.RequestedChunks > 0 {
		avg := ceilDiv(sourceBytes, int64(policy.RequestedChunks))
		if avg > policy.MaxChunkSize {
			return nil, fmt.Errorf("requested chunk count %d would average %s per chunk, above max safe chunk size %s; increase --chunks", policy.RequestedChunks, formatBytes(avg), formatBytes(policy.MaxChunkSize))
		}
	}

	words, entryBytes, err := scanWordlistStats(path, policy.ScannerMaxTokenSize)
	if err != nil {
		return nil, err
	}
	if words == 0 {
		return nil, fmt.Errorf("no entries found in wordlist")
	}

	effective := effectiveChunkCount(sourceBytes, words, policy)
	return &Metadata{
		Path:             path,
		TotalWords:       words,
		TotalSourceBytes: sourceBytes,
		EffectiveChunks:  effective,
		RequestedChunks:  policy.RequestedChunks,
		TargetChunkSize:  policy.TargetChunkSize,
		MaxChunkSize:     policy.MaxChunkSize,
		totalEntryBytes:  entryBytes,
	}, nil
}

// SplitFile streams path into temporary chunk files under tempDir. Non-empty
// entries are preserved exactly except that chunks are newline-terminated.
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

func splitWithMetadata(meta *Metadata, tempDir string, policy Policy, keyForChunk func(int) string) (*Result, error) {
	if tempDir == "" {
		tempDir = os.TempDir()
	}
	if err := os.MkdirAll(tempDir, 0o700); err != nil {
		return nil, fmt.Errorf("creating wordlist temp dir: %w", err)
	}

	file, err := os.Open(meta.Path)
	if err != nil {
		return nil, fmt.Errorf("opening wordlist file: %w", err)
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
		currentFile *os.File
		writer      *bufio.Writer
		chunkIndex  int
		chunkBytes  int64
		chunkWords  int
		processed   int
		currentPath string
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
		chunkWords = 0
	}

	cleanupFailedSplit := func() {
		cleanupOpenChunk()
		_ = result.Cleanup()
	}

	startChunk := func() error {
		f, err := os.CreateTemp(tempDir, fmt.Sprintf("chunk_%06d_*.txt", chunkIndex))
		if err != nil {
			return fmt.Errorf("creating wordlist chunk %d: %w", chunkIndex, err)
		}
		currentFile = f
		writer = bufio.NewWriter(f)
		currentPath = f.Name()
		chunkBytes = 0
		chunkWords = 0
		return nil
	}

	finishChunk := func() error {
		if currentFile == nil {
			return nil
		}
		if err := writer.Flush(); err != nil {
			cleanupOpenChunk()
			return fmt.Errorf("flushing wordlist chunk %d: %w", chunkIndex, err)
		}
		if err := currentFile.Close(); err != nil {
			cleanupOpenChunk()
			return fmt.Errorf("closing wordlist chunk %d: %w", chunkIndex, err)
		}
		key := ""
		if keyForChunk != nil {
			key = keyForChunk(chunkIndex)
		}
		result.Chunks = append(result.Chunks, Chunk{
			Path:        currentPath,
			Key:         key,
			ByteSize:    chunkBytes,
			WordCount:   chunkWords,
			Index:       chunkIndex,
			TotalChunks: meta.EffectiveChunks,
		})
		currentFile = nil
		writer = nil
		currentPath = ""
		return nil
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		entryBytes := int64(len(line) + 1)
		if entryBytes > policy.MaxChunkSize {
			cleanupFailedSplit()
			return result, fmt.Errorf("wordlist entry is %s, above max safe chunk size %s", formatBytes(entryBytes), formatBytes(policy.MaxChunkSize))
		}

		futureChunks := meta.EffectiveChunks - chunkIndex - 1
		if currentFile != nil && chunkWords > 0 && chunkBytes+entryBytes > policy.MaxChunkSize {
			if futureChunks <= 0 {
				cleanupFailedSplit()
				return result, fmt.Errorf("wordlist chunk %d would exceed max safe chunk size %s; increase --chunks", chunkIndex, formatBytes(policy.MaxChunkSize))
			}
			if err := finishChunk(); err != nil {
				cleanupFailedSplit()
				return result, err
			}
			chunkIndex++
			futureChunks = meta.EffectiveChunks - chunkIndex - 1
		}

		remainingWordsIncludingLine := meta.TotalWords - processed
		sizeSplit := currentFile != nil && chunkWords > 0 && chunkBytes+entryBytes > targetBytes && futureChunks > 0
		forceSplit := currentFile != nil && chunkWords > 0 && futureChunks > 0 && remainingWordsIncludingLine <= futureChunks
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
		if _, err := writer.WriteString(line); err != nil {
			cleanupFailedSplit()
			return result, fmt.Errorf("writing wordlist chunk %d: %w", chunkIndex, err)
		}
		if err := writer.WriteByte('\n'); err != nil {
			cleanupFailedSplit()
			return result, fmt.Errorf("writing wordlist chunk %d: %w", chunkIndex, err)
		}
		chunkBytes += entryBytes
		chunkWords++
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

func scanWordlistStats(path string, scannerMax int) (int, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0, fmt.Errorf("opening wordlist file: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	scanner := newScanner(file, scannerMax)
	var words int
	var entryBytes int64
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		words++
		entryBytes += int64(len(line) + 1)
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, scannerError(err, scannerMax)
	}
	return words, entryBytes, nil
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
		return fmt.Errorf("wordlist line exceeds scanner max token size %s", formatBytes(int64(maxTokenSize)))
	}
	return fmt.Errorf("scanning wordlist file: %w", err)
}

func effectiveChunkCount(sourceBytes int64, totalWords int, policy Policy) int {
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
	if totalWords > 0 && desired > totalWords {
		desired = totalWords
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
	if n <= 0 {
		return 0
	}
	return (n + d - 1) / d
}

func formatBytes(n int64) string {
	const mib = 1024 * 1024
	if n%mib == 0 {
		return fmt.Sprintf("%d MiB", n/mib)
	}
	return fmt.Sprintf("%d bytes", n)
}
