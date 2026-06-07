package wordlist

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	tempDirPrefix       = "heph-wordlist-"
	DefaultStaleTempAge = 24 * time.Hour
)

// CleanupStaleTempDirs removes stale wordlist temp directories from os.TempDir.
func CleanupStaleTempDirs(maxAge time.Duration) error {
	return cleanupStaleTempDirs(os.TempDir(), time.Now(), maxAge)
}

func cleanupStaleTempDirs(root string, now time.Time, maxAge time.Duration) error {
	if maxAge <= 0 {
		maxAge = DefaultStaleTempAge
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("reading temp dir %s: %w", root, err)
	}

	var errs []error
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), tempDirPrefix) {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			errs = append(errs, fmt.Errorf("stat stale wordlist temp dir %s: %w", filepath.Join(root, entry.Name()), err))
			continue
		}
		if now.Sub(info.ModTime()) < maxAge {
			continue
		}
		path := filepath.Join(root, entry.Name())
		if err := os.RemoveAll(path); err != nil {
			errs = append(errs, fmt.Errorf("removing stale wordlist temp dir %s: %w", path, err))
		}
	}
	return errors.Join(errs...)
}
