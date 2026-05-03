package bench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const appName = "heph4estus"

type Store struct {
	dir string
}

func NewStore() (*Store, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("resolving user config dir: %w", err)
	}
	return NewStoreAt(filepath.Join(base, appName, "benchmarks")), nil
}

func NewStoreAt(dir string) *Store {
	return &Store{dir: dir}
}

func (s *Store) Save(report FleetReport) (string, error) {
	if report.Tool == "" {
		return "", fmt.Errorf("benchmark report tool is required")
	}
	if report.Cloud == "" {
		return "", fmt.Errorf("benchmark report cloud is required")
	}
	if report.GeneratedAt.IsZero() {
		report.GeneratedAt = time.Now().UTC()
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return "", fmt.Errorf("creating benchmark store dir: %w", err)
	}
	filename := fmt.Sprintf("%s-%s-%s.json",
		report.GeneratedAt.UTC().Format("20060102T150405Z"),
		sanitizePathToken(report.Cloud),
		sanitizePathToken(report.Tool),
	)
	path := filepath.Join(s.dir, filename)
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshaling benchmark report: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("writing benchmark report: %w", err)
	}
	return path, nil
}

func (s *Store) Load(path string) (*FleetReport, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading benchmark report: %w", err)
	}
	var report FleetReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("parsing benchmark report: %w", err)
	}
	return &report, nil
}

func (s *Store) List(tool, cloud string, limit int) ([]FleetReport, error) {
	entries, err := os.ReadDir(s.dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("listing benchmark reports: %w", err)
	}
	var reports []FleetReport
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		report, err := s.Load(filepath.Join(s.dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		if tool != "" && report.Tool != tool {
			continue
		}
		if cloud != "" && report.Cloud != cloud {
			continue
		}
		reports = append(reports, *report)
	}
	sort.Slice(reports, func(i, j int) bool {
		return reports[i].GeneratedAt.After(reports[j].GeneratedAt)
	})
	if limit > 0 && len(reports) > limit {
		reports = reports[:limit]
	}
	return reports, nil
}

func sanitizePathToken(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	raw = strings.ReplaceAll(raw, "/", "-")
	raw = strings.ReplaceAll(raw, " ", "-")
	if raw == "" {
		return "unknown"
	}
	return raw
}
