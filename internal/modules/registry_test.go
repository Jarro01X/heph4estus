package modules

import (
	"errors"
	"testing"
	"testing/fstest"
)

func TestNewRegistry_Empty(t *testing.T) {
	r := NewRegistry()
	if got := r.List(); len(got) != 0 {
		t.Fatalf("expected empty list, got %d items", len(got))
	}
	if got := r.Names(); len(got) != 0 {
		t.Fatalf("expected empty names, got %d items", len(got))
	}
}

func TestRegistry_AddAndGet(t *testing.T) {
	r := NewRegistry()
	m := validModule()
	if err := r.Add(m); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := r.Get("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "test" {
		t.Fatalf("expected name %q, got %q", "test", got.Name)
	}
	if got.Command != m.Command {
		t.Fatalf("expected command %q, got %q", m.Command, got.Command)
	}
}

func TestRegistry_GetNotFound(t *testing.T) {
	r := NewRegistry()
	_, err := r.Get("nonexistent")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrModuleNotFound) {
		t.Fatalf("expected ErrModuleNotFound, got %v", err)
	}
}

func TestRegistry_AddDuplicate(t *testing.T) {
	r := NewRegistry()
	m := validModule()
	if err := r.Add(m); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	err := r.Add(m)
	if err == nil {
		t.Fatal("expected error on duplicate, got nil")
	}
	if !errors.Is(err, ErrInvalidModule) {
		t.Fatalf("expected ErrInvalidModule, got %v", err)
	}
}

func TestRegistry_AddInvalid(t *testing.T) {
	r := NewRegistry()
	m := ModuleDefinition{Name: "bad"}
	err := r.Add(m)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrInvalidModule) {
		t.Fatalf("expected ErrInvalidModule, got %v", err)
	}
}

func TestRegistry_ListSorted(t *testing.T) {
	r := NewRegistry()
	names := []string{"charlie", "alpha", "bravo"}
	for _, n := range names {
		m := validModule()
		m.Name = n
		if err := r.Add(m); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	list := r.List()
	if len(list) != 3 {
		t.Fatalf("expected 3 items, got %d", len(list))
	}
	expected := []string{"alpha", "bravo", "charlie"}
	for i, e := range expected {
		if list[i].Name != e {
			t.Fatalf("position %d: expected %q, got %q", i, e, list[i].Name)
		}
	}
}

func TestRegistry_ListByTag(t *testing.T) {
	r := NewRegistry()

	m1 := validModule()
	m1.Name = "tool_a"
	m1.Tags = []string{"web", "recon"}

	m2 := validModule()
	m2.Name = "tool_b"
	m2.Tags = []string{"network"}

	m3 := validModule()
	m3.Name = "tool_c"
	m3.Tags = []string{"web", "fuzzer"}

	for _, m := range []ModuleDefinition{m1, m2, m3} {
		if err := r.Add(m); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	web := r.ListByTag("web")
	if len(web) != 2 {
		t.Fatalf("expected 2 web modules, got %d", len(web))
	}
	if web[0].Name != "tool_a" || web[1].Name != "tool_c" {
		t.Fatalf("expected [tool_a, tool_c], got [%s, %s]", web[0].Name, web[1].Name)
	}

	network := r.ListByTag("network")
	if len(network) != 1 || network[0].Name != "tool_b" {
		t.Fatalf("expected [tool_b], got %v", network)
	}
}

func TestRegistry_ListByTagEmpty(t *testing.T) {
	r := NewRegistry()
	m := validModule()
	if err := r.Add(m); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := r.ListByTag("nonexistent")
	if len(got) != 0 {
		t.Fatalf("expected empty list, got %d items", len(got))
	}
}

func TestRegistry_Names(t *testing.T) {
	r := NewRegistry()
	for _, n := range []string{"zulu", "alpha", "mike"} {
		m := validModule()
		m.Name = n
		if err := r.Add(m); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	names := r.Names()
	expected := []string{"alpha", "mike", "zulu"}
	if len(names) != 3 {
		t.Fatalf("expected 3 names, got %d", len(names))
	}
	for i, e := range expected {
		if names[i] != e {
			t.Fatalf("position %d: expected %q, got %q", i, e, names[i])
		}
	}
}

func TestRegistry_LoadFS(t *testing.T) {
	yaml := `name: testmod
description: A test module
command: "test -i {{input}} -o {{output}}"
input_type: target_list
output_ext: json
install_cmd: "apk add test"
default_cpu: 256
default_memory: 512
timeout: 5m
tags: [scanner]
`
	testFS := fstest.MapFS{
		"defs/testmod.yaml": &fstest.MapFile{Data: []byte(yaml)},
	}

	r := NewRegistry()
	if err := r.LoadFS(testFS, "defs"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := r.Get("testmod")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "testmod" {
		t.Fatalf("expected name %q, got %q", "testmod", got.Name)
	}
}

func TestRegistry_LoadFS_InvalidYAML(t *testing.T) {
	testFS := fstest.MapFS{
		"defs/bad.yaml": &fstest.MapFile{Data: []byte("{{{{not yaml")},
	}

	r := NewRegistry()
	err := r.LoadFS(testFS, "defs")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestRegistry_LoadFS_ValidationFailure(t *testing.T) {
	yaml := `name: incomplete
description: Missing required fields
`
	testFS := fstest.MapFS{
		"defs/incomplete.yaml": &fstest.MapFile{Data: []byte(yaml)},
	}

	r := NewRegistry()
	err := r.LoadFS(testFS, "defs")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestNewDefaultRegistry(t *testing.T) {
	r, err := NewDefaultRegistry()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	names := r.Names()
	if len(names) != 14 {
		t.Fatalf("expected 14 modules, got %d: %v", len(names), names)
	}

	// Every built-in module must pass validation (already validated by Add, but smoke-test)
	for _, def := range r.List() {
		if err := def.Validate(); err != nil {
			t.Errorf("module %q failed validation: %v", def.Name, err)
		}
	}
}

func TestNewDefaultRegistry_KnownModules(t *testing.T) {
	r, err := NewDefaultRegistry()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{
		"dalfox", "dnsx", "feroxbuster", "ffuf", "gobuster",
		"gospider", "gowitness", "httpx", "katana", "massdns",
		"masscan", "nmap", "nuclei", "subfinder",
	}
	for _, name := range expected {
		if _, err := r.Get(name); err != nil {
			t.Errorf("expected module %q to exist: %v", name, err)
		}
	}
}

func TestRegistry_GetReturnsCopy(t *testing.T) {
	r := NewRegistry()
	m := validModule()
	if err := r.Add(m); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, _ := r.Get("test")
	got.Name = "mutated"

	original, _ := r.Get("test")
	if original.Name != "test" {
		t.Fatal("Get returned a reference, not a copy — mutation affected registry")
	}
}
