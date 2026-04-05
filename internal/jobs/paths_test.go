package jobs

import (
	"strings"
	"testing"
)

func TestNewID(t *testing.T) {
	got := NewID("nmap")
	if !strings.HasPrefix(got, "nmap-") {
		t.Fatalf("expected nmap prefix, got %q", got)
	}
	if strings.Contains(got, "/") {
		t.Fatalf("job id must be path-safe, got %q", got)
	}
}

func TestResultPrefix(t *testing.T) {
	got := ResultPrefix("nmap", "job-123")
	want := "scans/nmap/job-123/results/"
	if got != want {
		t.Fatalf("ResultPrefix() = %q, want %q", got, want)
	}
}

func TestResultAndArtifactKeys(t *testing.T) {
	resultKey := ResultKey("nmap", "job-123", "example.com", "example.com_line1", 2, 5, 1700000000, "json")
	wantResult := "scans/nmap/job-123/results/example.com_line1/example.com_chunk2_of_5_1700000000.json"
	if resultKey != wantResult {
		t.Fatalf("ResultKey() = %q, want %q", resultKey, wantResult)
	}

	artifactKey := ArtifactKey("nmap", "job-123", "example.com", "", 0, 0, 1700000000, "xml")
	wantArtifact := "scans/nmap/job-123/artifacts/example.com_1700000000.xml"
	if artifactKey != wantArtifact {
		t.Fatalf("ArtifactKey() = %q, want %q", artifactKey, wantArtifact)
	}
}

func TestTargetFromKey(t *testing.T) {
	tests := []struct {
		key  string
		want string
	}{
		{"scans/nmap/job-123/results/10.0.0.1_1709913600.json", "10.0.0.1"},
		{"scans/nmap/job-123/results/example.com_line1/example.com_chunk0_of_5_1700000000.json", "example.com"},
		{"scans/nmap/job-123/artifacts/example.com_1709913600.xml", "example.com"},
	}

	for _, tt := range tests {
		if got := TargetFromKey(tt.key); got != tt.want {
			t.Errorf("TargetFromKey(%q) = %q, want %q", tt.key, got, tt.want)
		}
	}
}
