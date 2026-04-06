package modules

import (
	"errors"
	"testing"
	"time"
)

func validModule() ModuleDefinition {
	return ModuleDefinition{
		Name:          "test",
		Description:   "A test module",
		Exec:          []string{"test", "-i", "{{input}}", "-o", "{{output}}"},
		InputType:     InputTypeTargetList,
		OutputExt:     "json",
		InstallCmd:    "apk add --no-cache test",
		DefaultCPU:    256,
		DefaultMemory: 512,
		Timeout:       "5m",
		Tags:          []string{"scanner"},
	}
}

func TestValidate_Valid(t *testing.T) {
	m := validModule()
	if err := m.Validate(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidate_MissingFields(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*ModuleDefinition)
	}{
		{"missing name", func(m *ModuleDefinition) { m.Name = "" }},
		{"missing exec", func(m *ModuleDefinition) { m.Exec = nil }},
		{"missing input_type", func(m *ModuleDefinition) { m.InputType = "" }},
		{"missing output_ext", func(m *ModuleDefinition) { m.OutputExt = "" }},
		{"missing install_cmd", func(m *ModuleDefinition) { m.InstallCmd = "" }},
		{"missing timeout", func(m *ModuleDefinition) { m.Timeout = "" }},
		{"zero cpu", func(m *ModuleDefinition) { m.DefaultCPU = 0 }},
		{"zero memory", func(m *ModuleDefinition) { m.DefaultMemory = 0 }},
		{"negative cpu", func(m *ModuleDefinition) { m.DefaultCPU = -1 }},
		{"negative memory", func(m *ModuleDefinition) { m.DefaultMemory = -1 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := validModule()
			tt.modify(&m)
			err := m.Validate()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, ErrInvalidModule) {
				t.Fatalf("expected ErrInvalidModule, got %v", err)
			}
		})
	}
}

func TestValidate_ShellIsValid(t *testing.T) {
	m := validModule()
	m.Exec = nil
	m.Shell = "tool {{target}} > {{output}}"
	if err := m.Validate(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidate_ExecAndShellMutuallyExclusive(t *testing.T) {
	m := validModule()
	m.Shell = "tool {{target}}"
	err := m.Validate()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrInvalidModule) {
		t.Fatalf("expected ErrInvalidModule, got %v", err)
	}
}

func TestValidate_InvalidInputType(t *testing.T) {
	m := validModule()
	m.InputType = "foobar"
	err := m.Validate()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrInvalidModule) {
		t.Fatalf("expected ErrInvalidModule, got %v", err)
	}
}

func TestValidate_InvalidTimeout(t *testing.T) {
	m := validModule()
	m.Timeout = "notaduration"
	err := m.Validate()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrInvalidModule) {
		t.Fatalf("expected ErrInvalidModule, got %v", err)
	}
}

func TestValidate_ValidTimeoutFormats(t *testing.T) {
	formats := []string{"5m", "1h30m", "30s", "2h", "500ms"}
	for _, f := range formats {
		t.Run(f, func(t *testing.T) {
			m := validModule()
			m.Timeout = f
			if err := m.Validate(); err != nil {
				t.Fatalf("expected no error for timeout %q, got %v", f, err)
			}
		})
	}
}

func TestValidate_WordlistInputType(t *testing.T) {
	m := validModule()
	m.InputType = InputTypeWordlist
	if err := m.Validate(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidate_EmptyEnvIsValid(t *testing.T) {
	m := validModule()
	m.Env = map[string]string{}
	if err := m.Validate(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidate_EmptyDescriptionIsValid(t *testing.T) {
	m := validModule()
	m.Description = ""
	if err := m.Validate(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestNeedsTarget(t *testing.T) {
	// Exec-based module with {{target}}
	m := ModuleDefinition{
		Exec: []string{"tool", "-u", "{{target}}", "-w", "{{input}}"},
	}
	if !m.NeedsTarget() {
		t.Error("expected NeedsTarget() = true for module with {{target}}")
	}

	// Module without {{target}}
	m2 := ModuleDefinition{
		Exec: []string{"tool", "-w", "{{input}}", "-o", "{{output}}"},
	}
	if m2.NeedsTarget() {
		t.Error("expected NeedsTarget() = false for module without {{target}}")
	}

	// Shell-based module with {{target}}
	m3 := ModuleDefinition{
		Shell: "tool -u {{target}} -w {{wordlist}}",
	}
	if !m3.NeedsTarget() {
		t.Error("expected NeedsTarget() = true for shell module with {{target}}")
	}
}

func TestNeedsWordlist(t *testing.T) {
	// Module with {{wordlist}}
	m := ModuleDefinition{
		Exec: []string{"ffuf", "-w", "{{wordlist}}", "-u", "{{target}}"},
	}
	if !m.NeedsWordlist() {
		t.Error("expected NeedsWordlist() = true for module with {{wordlist}}")
	}

	// Module with {{input}}
	m2 := ModuleDefinition{
		Exec: []string{"tool", "-f", "{{input}}"},
	}
	if !m2.NeedsWordlist() {
		t.Error("expected NeedsWordlist() = true for module with {{input}}")
	}

	// Module without wordlist
	m3 := ModuleDefinition{
		Exec: []string{"tool", "{{target}}"},
	}
	if m3.NeedsWordlist() {
		t.Error("expected NeedsWordlist() = false for module without {{input}} or {{wordlist}}")
	}
}

func TestTimeoutDuration(t *testing.T) {
	m := validModule()
	m.Timeout = "5m"
	if got := m.TimeoutDuration(); got != 5*time.Minute {
		t.Fatalf("expected 5m, got %v", got)
	}

	m.Timeout = "1h30m"
	if got := m.TimeoutDuration(); got != 90*time.Minute {
		t.Fatalf("expected 1h30m, got %v", got)
	}
}
