package wordlist

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanupStaleTempDirsRemovesOnlyStaleWordlistDirs(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	stale := filepath.Join(root, "heph-wordlist-stale")
	fresh := filepath.Join(root, "heph-wordlist-fresh")
	other := filepath.Join(root, "other-stale")
	for _, dir := range []string{stale, fresh, other} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	old := now.Add(-48 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatalf("chtimes stale: %v", err)
	}
	if err := os.Chtimes(other, old, old); err != nil {
		t.Fatalf("chtimes other: %v", err)
	}

	if err := cleanupStaleTempDirs(root, now, 24*time.Hour); err != nil {
		t.Fatalf("cleanup stale temp dirs: %v", err)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale wordlist dir should be removed, stat err=%v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("fresh wordlist dir should remain: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("non-matching dir should remain: %v", err)
	}
}

func TestCleanupStaleTempDirsIgnoresFilesAndSymlinks(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	old := now.Add(-48 * time.Hour)

	filePath := filepath.Join(root, "heph-wordlist-file")
	if err := os.WriteFile(filePath, []byte("data"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.Chtimes(filePath, old, old); err != nil {
		t.Fatalf("chtimes file: %v", err)
	}

	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	link := filepath.Join(root, "heph-wordlist-link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink not available: %v", err)
	}

	if err := cleanupStaleTempDirs(root, now, 24*time.Hour); err != nil {
		t.Fatalf("cleanup stale temp dirs: %v", err)
	}
	if _, err := os.Lstat(filePath); err != nil {
		t.Fatalf("matching file should remain: %v", err)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Fatalf("matching symlink should remain: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("symlink target should remain: %v", err)
	}
}

func TestCleanupStaleTempDirsDefaultsMaxAge(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	stale := filepath.Join(root, "heph-wordlist-stale")
	if err := os.Mkdir(stale, 0o700); err != nil {
		t.Fatalf("mkdir stale: %v", err)
	}
	old := now.Add(-DefaultStaleTempAge - time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatalf("chtimes stale: %v", err)
	}

	if err := cleanupStaleTempDirs(root, now, 0); err != nil {
		t.Fatalf("cleanup stale temp dirs: %v", err)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale wordlist dir should be removed with default age, stat err=%v", err)
	}
}
