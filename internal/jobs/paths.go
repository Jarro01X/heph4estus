package jobs

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"strings"
	"time"
)

const legacyJobID = "legacy"

// NewID returns a stable-enough identifier for a submitted scan job.
func NewID(tool string) string {
	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return fmt.Sprintf("%s-%d", sanitizeSegment(tool, "job"), time.Now().UTC().UnixNano())
	}
	return fmt.Sprintf("%s-%s-%s",
		sanitizeSegment(tool, "job"),
		time.Now().UTC().Format("20060102t150405"),
		hex.EncodeToString(suffix[:]),
	)
}

func ResultPrefix(toolName, jobID string) string {
	return path.Join("scans", sanitizeSegment(toolName, "tool"), normalizeJobID(jobID), "results") + "/"
}

func ArtifactPrefix(toolName, jobID string) string {
	return path.Join("scans", sanitizeSegment(toolName, "tool"), normalizeJobID(jobID), "artifacts") + "/"
}

func ResultKey(toolName, jobID, target, groupID string, chunkIdx, totalChunks int, ts int64, ext string) string {
	return path.Join(ResultPrefix(toolName, jobID), resultFileName(target, groupID, chunkIdx, totalChunks, ts, ext))
}

func ArtifactKey(toolName, jobID, target, groupID string, chunkIdx, totalChunks int, ts int64, ext string) string {
	return path.Join(ArtifactPrefix(toolName, jobID), resultFileName(target, groupID, chunkIdx, totalChunks, ts, ext))
}

// TargetFromKey extracts the original target from any result or artifact key.
func TargetFromKey(key string) string {
	base := path.Base(key)
	base = strings.TrimSuffix(base, path.Ext(base))
	if chunkIdx := strings.Index(base, "_chunk"); chunkIdx > 0 {
		return base[:chunkIdx]
	}
	if idx := strings.LastIndex(base, "_"); idx > 0 {
		return base[:idx]
	}
	return base
}

func normalizeJobID(jobID string) string {
	return sanitizeSegment(jobID, legacyJobID)
}

func sanitizeSegment(segment, fallback string) string {
	segment = strings.TrimSpace(segment)
	segment = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		case r >= '0' && r <= '9':
			return r
		case r == '-' || r == '_' || r == '.':
			return r
		default:
			return '-'
		}
	}, segment)
	segment = strings.Trim(segment, "-.")
	if segment == "" {
		return fallback
	}
	return segment
}

// InputPrefix returns the S3 key prefix for uploaded wordlist chunks.
func InputPrefix(toolName, jobID string) string {
	return path.Join("scans", sanitizeSegment(toolName, "tool"), normalizeJobID(jobID), "inputs") + "/"
}

// InputKey returns the S3 key for a specific wordlist chunk file.
func InputKey(toolName, jobID string, chunkIdx int) string {
	return path.Join(InputPrefix(toolName, jobID), fmt.Sprintf("chunk_%d.txt", chunkIdx))
}

// SafeTargetStem returns a path-safe representation of a target string.
// URL-shaped targets are sanitized to avoid bad S3 key paths.
func SafeTargetStem(target string) string {
	trimmed := strings.TrimSpace(target)
	safe := sanitizeSegment(trimmed, "target")
	if trimmed == "" || trimmed == safe {
		return safe
	}
	return fmt.Sprintf("%s-%s", safe, shortHash(trimmed))
}

func resultFileName(target, groupID string, chunkIdx, totalChunks int, ts int64, ext string) string {
	safe := SafeTargetStem(target)
	file := fmt.Sprintf("%s_%d.%s", safe, ts, ext)
	if groupID != "" {
		safeGroup := sanitizeSegment(groupID, "group")
		file = path.Join(safeGroup, fmt.Sprintf("%s_chunk%d_of_%d_%d.%s", safe, chunkIdx, totalChunks, ts, ext))
	}
	return file
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:4])
}
