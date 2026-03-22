package nmap

import (
	"encoding/json"
	"testing"
)

func TestParseTargetsWithMode_TargetOnly(t *testing.T) {
	s := NewScanner(nil)

	content := "example.com -sS -p 80,443\n10.0.0.1\n"
	tasks := s.ParseTargetsWithMode(content, "-sS", "target-only", 5)

	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
	if tasks[0].Target != "example.com" {
		t.Errorf("expected target example.com, got %s", tasks[0].Target)
	}
	if tasks[0].Options != "-sS -p 80,443" {
		t.Errorf("expected options '-sS -p 80,443', got %q", tasks[0].Options)
	}
	if tasks[0].GroupID != "" || tasks[0].ChunkIdx != 0 || tasks[0].TotalChunks != 0 {
		t.Error("expected empty chunk fields in target-only mode")
	}
	if tasks[1].Target != "10.0.0.1" {
		t.Errorf("expected target 10.0.0.1, got %s", tasks[1].Target)
	}
	if tasks[1].Options != "-sS" {
		t.Errorf("expected default options '-sS', got %q", tasks[1].Options)
	}
}

func TestParseTargetsWithMode_EmptyMode(t *testing.T) {
	s := NewScanner(nil)
	tasks := s.ParseTargetsWithMode("example.com\n", "-sS", "", 5)
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].GroupID != "" {
		t.Error("empty mode should behave as target-only")
	}
}

func TestParseTargetsWithMode_TargetPorts_ExplicitPort(t *testing.T) {
	s := NewScanner(nil)

	content := "example.com -sS -p 1-1000\n"
	tasks := s.ParseTargetsWithMode(content, "-sS", "target-ports", 5)

	if len(tasks) != 5 {
		t.Fatalf("expected 5 tasks, got %d", len(tasks))
	}

	for i, task := range tasks {
		if task.Target != "example.com" {
			t.Errorf("task %d: expected target example.com, got %s", i, task.Target)
		}
		if task.GroupID != "example.com_line1" {
			t.Errorf("task %d: expected GroupID example.com_line1, got %s", i, task.GroupID)
		}
		if task.ChunkIdx != i {
			t.Errorf("task %d: expected ChunkIdx %d, got %d", i, i, task.ChunkIdx)
		}
		if task.TotalChunks != 5 {
			t.Errorf("task %d: expected TotalChunks 5, got %d", i, task.TotalChunks)
		}
	}

	// First chunk should have -sS -p 1-200
	if tasks[0].Options != "-sS -p 1-200" {
		t.Errorf("task 0: expected options '-sS -p 1-200', got %q", tasks[0].Options)
	}
	// Last chunk should have -sS -p 801-1000
	if tasks[4].Options != "-sS -p 801-1000" {
		t.Errorf("task 4: expected options '-sS -p 801-1000', got %q", tasks[4].Options)
	}
}

func TestParseTargetsWithMode_TargetPorts_NoPortFlag(t *testing.T) {
	s := NewScanner(nil)

	content := "example.com -sS\n"
	tasks := s.ParseTargetsWithMode(content, "-sS", "target-ports", 5)

	if len(tasks) != 5 {
		t.Fatalf("expected 5 tasks (all-port split), got %d", len(tasks))
	}

	// Should default to 1-65535 split into 5 chunks
	if tasks[0].GroupID != "example.com_line1" {
		t.Errorf("expected GroupID example.com_line1, got %s", tasks[0].GroupID)
	}

	// Verify all ports are covered
	totalPorts := 0
	for _, task := range tasks {
		portSpec, _, found := ExtractPortFlag(task.Options)
		if !found {
			t.Errorf("expected -p flag in options %q", task.Options)
			continue
		}
		ports, err := ParsePortSpec(portSpec)
		if err != nil {
			t.Errorf("failed to parse port spec %q: %v", portSpec, err)
			continue
		}
		totalPorts += len(ports)
	}
	if totalPorts != 65535 {
		t.Errorf("expected 65535 total ports, got %d", totalPorts)
	}
}

func TestParseTargetsWithMode_TargetPorts_MultipleTargets(t *testing.T) {
	s := NewScanner(nil)

	content := "example.com -p 1-100\n10.0.0.1 -p 200-300\n"
	tasks := s.ParseTargetsWithMode(content, "-sS", "target-ports", 2)

	if len(tasks) != 4 {
		t.Fatalf("expected 4 tasks (2 targets x 2 chunks), got %d", len(tasks))
	}

	// First target, first chunk
	if tasks[0].GroupID != "example.com_line1" {
		t.Errorf("task 0: expected GroupID example.com_line1, got %s", tasks[0].GroupID)
	}
	// Second target, first chunk
	if tasks[2].GroupID != "10.0.0.1_line2" {
		t.Errorf("task 2: expected GroupID 10.0.0.1_line2, got %s", tasks[2].GroupID)
	}
}

func TestParseTargetsWithMode_TargetPorts_PreservesOptions(t *testing.T) {
	s := NewScanner(nil)

	content := "example.com -sS -T4 -p 1-10 --open\n"
	tasks := s.ParseTargetsWithMode(content, "-sS", "target-ports", 2)

	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}

	// -p should be extracted and replaced; other options preserved
	for _, task := range tasks {
		if task.Options == "" {
			t.Error("expected non-empty options")
		}
		// Should contain -sS, -T4, --open
		for _, flag := range []string{"-sS", "-T4", "--open"} {
			found := false
			for _, f := range []string{"-sS", "-T4", "--open"} {
				if f == flag {
					found = true
				}
			}
			if !found {
				t.Errorf("expected %s in options %q", flag, task.Options)
			}
		}
	}
}

func TestParseTargetsWithMode_TargetPorts_InvalidPortSpec(t *testing.T) {
	s := NewScanner(nil)

	// -p abc is invalid — should gracefully degrade to single task
	content := "example.com -p abc\n"
	tasks := s.ParseTargetsWithMode(content, "-sS", "target-ports", 5)

	if len(tasks) != 1 {
		t.Fatalf("expected 1 task (graceful degradation), got %d", len(tasks))
	}
	if tasks[0].GroupID != "" {
		t.Error("expected empty GroupID for unsplit task")
	}
	if tasks[0].Options != "-p abc" {
		t.Errorf("expected original options '-p abc', got %q", tasks[0].Options)
	}
}

func TestParseTargetsWithMode_TargetPorts_MoreChunksThanPorts(t *testing.T) {
	s := NewScanner(nil)

	content := "example.com -p 80,443\n"
	tasks := s.ParseTargetsWithMode(content, "-sS", "target-ports", 10)

	// Only 2 ports, should clamp to 2 chunks
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks (clamped), got %d", len(tasks))
	}
	if tasks[0].TotalChunks != 2 {
		t.Errorf("expected TotalChunks 2, got %d", tasks[0].TotalChunks)
	}
}

func TestParseTargetsWithMode_TargetPorts_SinglePort(t *testing.T) {
	s := NewScanner(nil)

	content := "example.com -p 80\n"
	tasks := s.ParseTargetsWithMode(content, "-sS", "target-ports", 5)

	if len(tasks) != 1 {
		t.Fatalf("expected 1 task (single port), got %d", len(tasks))
	}
	if tasks[0].TotalChunks != 1 {
		t.Errorf("expected TotalChunks 1, got %d", tasks[0].TotalChunks)
	}
}

func TestParseTargetsWithMode_SkipsCommentsAndEmpty(t *testing.T) {
	s := NewScanner(nil)

	content := "# comment\n\nexample.com -p 1-10\n\n# another comment\n10.0.0.1 -p 1-10\n"
	tasks := s.ParseTargetsWithMode(content, "-sS", "target-ports", 2)

	if len(tasks) != 4 {
		t.Fatalf("expected 4 tasks (2 targets x 2 chunks), got %d", len(tasks))
	}

	// Comments and empty lines should not affect line numbering
	if tasks[0].GroupID != "example.com_line1" {
		t.Errorf("expected GroupID example.com_line1, got %s", tasks[0].GroupID)
	}
	if tasks[2].GroupID != "10.0.0.1_line2" {
		t.Errorf("expected GroupID 10.0.0.1_line2, got %s", tasks[2].GroupID)
	}
}

func TestScanTask_JSON_TargetOnly(t *testing.T) {
	task := ScanTask{Target: "example.com", Options: "-sS"}
	b, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}

	s := string(b)
	// omitempty fields should be absent
	if contains(s, "group_id") || contains(s, "chunk_idx") || contains(s, "total_chunks") {
		t.Errorf("expected omitempty fields absent, got %s", s)
	}
}

func TestScanTask_JSON_TargetPorts(t *testing.T) {
	task := ScanTask{
		Target:      "example.com",
		Options:     "-sS -p 1-200",
		GroupID:     "example.com_line1",
		ChunkIdx:    1,
		TotalChunks: 5,
	}
	b, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}

	s := string(b)
	if !contains(s, `"group_id":"example.com_line1"`) {
		t.Errorf("expected group_id in JSON, got %s", s)
	}
	if !contains(s, `"chunk_idx":1`) {
		t.Errorf("expected chunk_idx in JSON, got %s", s)
	}
	if !contains(s, `"total_chunks":5`) {
		t.Errorf("expected total_chunks in JSON, got %s", s)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
