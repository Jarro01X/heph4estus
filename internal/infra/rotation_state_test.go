package infra

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadRotationAutoVarsMissing(t *testing.T) {
	vars, err := ReadRotationAutoVars(t.TempDir())
	if err != nil {
		t.Fatalf("ReadRotationAutoVars: %v", err)
	}
	if len(vars) != 0 {
		t.Fatalf("vars = %v, want empty", vars)
	}
}

func TestMergeRotationAutoVars(t *testing.T) {
	dir := t.TempDir()
	if err := WriteRotationAutoVars(dir, map[string]string{"existing": "keep"}); err != nil {
		t.Fatalf("WriteRotationAutoVars: %v", err)
	}
	vars, err := MergeRotationAutoVars(dir, map[string]string{"new": "value", "empty": ""})
	if err != nil {
		t.Fatalf("MergeRotationAutoVars: %v", err)
	}
	if vars["existing"] != "keep" || vars["new"] != "value" {
		t.Fatalf("vars = %v", vars)
	}
	if _, ok := vars["empty"]; ok {
		t.Fatalf("empty update should be skipped: %v", vars)
	}
	info, err := os.Stat(filepath.Join(dir, RotationAutoVarsFileName))
	if err != nil {
		t.Fatalf("stat rotation auto vars: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("permissions = %o, want 0600", info.Mode().Perm())
	}
}
