package settings

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"heph4estus/internal/doctor"
	"heph4estus/internal/operator"
	"heph4estus/internal/tui/core"
)

// --- helpers ---

func savedConfig(cfg *operator.OperatorConfig) func() (*operator.OperatorConfig, error) {
	return func() (*operator.OperatorConfig, error) { return cfg, nil }
}

func saveToMap(out *operator.OperatorConfig) func(*operator.OperatorConfig) error {
	return func(cfg *operator.OperatorConfig) error {
		*out = *cfg
		return nil
	}
}

func saveFail(msg string) func(*operator.OperatorConfig) error {
	return func(*operator.OperatorConfig) error { return fmt.Errorf("%s", msg) }
}

func stubDoctor(results []doctor.CheckResult) func(context.Context) []doctor.CheckResult {
	return func(context.Context) []doctor.CheckResult { return results }
}

func testDeps(cfg *operator.OperatorConfig) Deps {
	return Deps{
		LoadConfig: savedConfig(cfg),
		SaveConfig: func(*operator.OperatorConfig) error { return nil },
		RunDoctor:  stubDoctor(nil),
	}
}

// --- constructor ---

func TestNew_LoadsSavedConfig(t *testing.T) {
	cfg := &operator.OperatorConfig{
		Region:      "eu-west-1",
		Profile:     "staging",
		WorkerCount: 20,
		ComputeMode: "spot",
	}
	m := NewWithDeps(testDeps(cfg))

	if v := m.inputs[fieldRegion].Value(); v != "eu-west-1" {
		t.Errorf("region: got %q, want eu-west-1", v)
	}
	if v := m.inputs[fieldProfile].Value(); v != "staging" {
		t.Errorf("profile: got %q, want staging", v)
	}
	if v := m.inputs[fieldWorkerCount].Value(); v != "20" {
		t.Errorf("worker count: got %q, want 20", v)
	}
	if v := m.inputs[fieldComputeMode].Value(); v != "spot" {
		t.Errorf("compute mode: got %q, want spot", v)
	}
}

func TestNew_EmptyConfig_EmptyFields(t *testing.T) {
	m := NewWithDeps(testDeps(&operator.OperatorConfig{}))

	if v := m.inputs[fieldRegion].Value(); v != "" {
		t.Errorf("region: got %q, want empty", v)
	}
	if v := m.inputs[fieldWorkerCount].Value(); v != "" {
		t.Errorf("worker count: got %q, want empty", v)
	}
}

func TestNew_NilConfig(t *testing.T) {
	deps := Deps{
		LoadConfig: func() (*operator.OperatorConfig, error) { return nil, fmt.Errorf("no config") },
		SaveConfig: func(*operator.OperatorConfig) error { return nil },
		RunDoctor:  stubDoctor(nil),
	}
	m := NewWithDeps(deps)
	// Should not panic, fields should be empty
	if v := m.inputs[fieldRegion].Value(); v != "" {
		t.Errorf("region: got %q, want empty", v)
	}
}

// --- save ---

func TestSave_PersistsConfig(t *testing.T) {
	var saved operator.OperatorConfig
	deps := Deps{
		LoadConfig: savedConfig(&operator.OperatorConfig{}),
		SaveConfig: saveToMap(&saved),
		RunDoctor:  stubDoctor(nil),
	}
	m := NewWithDeps(deps)

	// Set values
	m.inputs[fieldRegion].SetValue("us-west-2")
	m.inputs[fieldProfile].SetValue("prod")
	m.inputs[fieldWorkerCount].SetValue("50")
	m.inputs[fieldComputeMode].SetValue("fargate")
	m.inputs[fieldCleanupPolicy].SetValue("destroy-after")
	m.inputs[fieldOutputDir].SetValue("/tmp/out")

	// Trigger save
	cmd := m.save()
	msg := cmd()

	saveMsg, ok := msg.(configSavedMsg)
	if !ok {
		t.Fatalf("expected configSavedMsg, got %T", msg)
	}
	if saveMsg.err != nil {
		t.Fatalf("save error: %v", saveMsg.err)
	}

	if saved.Region != "us-west-2" {
		t.Errorf("region: got %q", saved.Region)
	}
	if saved.Profile != "prod" {
		t.Errorf("profile: got %q", saved.Profile)
	}
	if saved.WorkerCount != 50 {
		t.Errorf("worker count: got %d", saved.WorkerCount)
	}
	if saved.ComputeMode != "fargate" {
		t.Errorf("compute mode: got %q", saved.ComputeMode)
	}
	if saved.CleanupPolicy != "destroy-after" {
		t.Errorf("cleanup: got %q", saved.CleanupPolicy)
	}
	if saved.OutputDir != "/tmp/out" {
		t.Errorf("output dir: got %q", saved.OutputDir)
	}
}

func TestSave_Error(t *testing.T) {
	deps := Deps{
		LoadConfig: savedConfig(&operator.OperatorConfig{}),
		SaveConfig: saveFail("disk full"),
		RunDoctor:  stubDoctor(nil),
	}
	m := NewWithDeps(deps)
	cmd := m.save()
	msg := cmd().(configSavedMsg)
	if msg.err == nil {
		t.Fatal("expected save error")
	}
}

func TestUpdate_SaveButton(t *testing.T) {
	var saved operator.OperatorConfig
	deps := Deps{
		LoadConfig: savedConfig(&operator.OperatorConfig{}),
		SaveConfig: saveToMap(&saved),
		RunDoctor:  stubDoctor(nil),
	}
	m := NewWithDeps(deps)
	m.inputs[fieldRegion].SetValue("ap-south-1")
	m.focusIndex = fieldSave

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected save command")
	}
	msg := cmd()
	_, _ = m.Update(msg)

	if m.statusMsg != "Settings saved" {
		t.Errorf("status: got %q, want 'Settings saved'", m.statusMsg)
	}
}

// --- diagnostics ---

func TestDiagnostics_Render(t *testing.T) {
	results := []doctor.CheckResult{
		{Name: "terraform_binary", Status: doctor.StatusPass, Summary: "terraform found"},
		{Name: "docker_daemon", Status: doctor.StatusFail, Summary: "Docker not reachable", Fix: "Start Docker"},
	}
	deps := Deps{
		LoadConfig: savedConfig(&operator.OperatorConfig{}),
		SaveConfig: func(*operator.OperatorConfig) error { return nil },
		RunDoctor:  stubDoctor(results),
	}
	m := NewWithDeps(deps)

	// Simulate Init receiving diagnostics
	cmd := m.Init()
	// Init returns a batch; extract diagnostic result
	// For testing, just directly set the results
	m.diagResults = results

	view := m.View()
	if !strings.Contains(view, "terraform found") {
		t.Error("expected terraform check in view")
	}
	if !strings.Contains(view, "Docker not reachable") {
		t.Error("expected docker check in view")
	}
	if !strings.Contains(view, "Start Docker") {
		t.Error("expected fix text in view")
	}
	_ = cmd
}

func TestRefreshDiagnostics(t *testing.T) {
	callCount := 0
	results := []doctor.CheckResult{
		{Name: "test", Status: doctor.StatusPass, Summary: "ok"},
	}
	deps := Deps{
		LoadConfig: savedConfig(&operator.OperatorConfig{}),
		SaveConfig: func(*operator.OperatorConfig) error { return nil },
		RunDoctor: func(ctx context.Context) []doctor.CheckResult {
			callCount++
			return results
		},
	}
	m := NewWithDeps(deps)
	m.focusIndex = fieldRefreshDiag

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected diagnostics command")
	}
	if !m.diagLoading {
		t.Error("expected diagLoading = true")
	}

	msg := cmd()
	m.Update(msg)
	if m.diagLoading {
		t.Error("expected diagLoading = false after result")
	}
	if callCount < 1 {
		t.Error("expected RunDoctor to be called")
	}
}

// --- navigation ---

func TestEsc_NavigatesBack(t *testing.T) {
	m := NewWithDeps(testDeps(&operator.OperatorConfig{}))
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("expected navigate command")
	}
	msg := cmd()
	nav, ok := msg.(core.NavigateMsg)
	if !ok {
		t.Fatalf("expected NavigateMsg, got %T", msg)
	}
	if nav.Target != core.ViewMenu {
		t.Errorf("expected ViewMenu, got %d", nav.Target)
	}
}

// --- view rendering ---

func TestView_ShowsAllLabels(t *testing.T) {
	m := NewWithDeps(testDeps(&operator.OperatorConfig{Region: "us-east-1"}))
	view := m.View()

	for _, label := range []string{"Region:", "Profile:", "Worker Count:", "Compute Mode:", "Cleanup Policy:", "Output Dir:"} {
		if !strings.Contains(view, label) {
			t.Errorf("view missing label %q", label)
		}
	}
	if !strings.Contains(view, "Save") {
		t.Error("view missing Save button")
	}
	if !strings.Contains(view, "Refresh Diagnostics") {
		t.Error("view missing Refresh Diagnostics button")
	}
	if !strings.Contains(view, "Diagnostics") {
		t.Error("view missing Diagnostics section")
	}
}

func TestView_ShowsEffectiveRegion(t *testing.T) {
	m := NewWithDeps(testDeps(&operator.OperatorConfig{Region: "eu-central-1"}))
	view := m.View()
	if !strings.Contains(view, "Effective Region:") {
		t.Error("view missing effective region")
	}
}

// --- tab navigation ---

func TestTab_CyclesFields(t *testing.T) {
	m := NewWithDeps(testDeps(&operator.OperatorConfig{}))
	if m.focusIndex != 0 {
		t.Fatalf("initial focus: got %d, want 0", m.focusIndex)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.focusIndex != 1 {
		t.Errorf("after tab: got %d, want 1", m.focusIndex)
	}
}

// --- buildConfig ---

func TestBuildConfig(t *testing.T) {
	m := NewWithDeps(testDeps(&operator.OperatorConfig{}))
	m.inputs[fieldRegion].SetValue("us-west-2")
	m.inputs[fieldWorkerCount].SetValue("25")
	m.inputs[fieldComputeMode].SetValue("spot")
	m.inputs[fieldCleanupPolicy].SetValue("reuse")
	m.inputs[fieldOutputDir].SetValue("/data/results")

	cfg := m.buildConfig()
	if cfg.Region != "us-west-2" {
		t.Errorf("region: %q", cfg.Region)
	}
	if cfg.WorkerCount != 25 {
		t.Errorf("workers: %d", cfg.WorkerCount)
	}
	if cfg.ComputeMode != "spot" {
		t.Errorf("mode: %q", cfg.ComputeMode)
	}
	if cfg.CleanupPolicy != "reuse" {
		t.Errorf("cleanup: %q", cfg.CleanupPolicy)
	}
	if cfg.OutputDir != "/data/results" {
		t.Errorf("output: %q", cfg.OutputDir)
	}
}
