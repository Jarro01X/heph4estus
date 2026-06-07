package naabu

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"heph4estus/internal/cloud"
	naabutool "heph4estus/internal/tools/naabu"
	targetlisttool "heph4estus/internal/tools/targetlist"
	"heph4estus/internal/tui/core"
)

func targetListMsg(path string, targets int) targetListReadMsg {
	return targetListReadMsg{
		path: path,
		meta: &targetlisttool.Metadata{
			Path:            path,
			TotalTargets:    targets,
			EffectiveChunks: targets,
		},
	}
}

func TestNaabuConfigDefaultView(t *testing.T) {
	m := NewConfig()
	v := m.View()
	for _, want := range []string{"Naabu", "[x] combined", "Nmap Options", "Target File"} {
		if !strings.Contains(v, want) {
			t.Fatalf("expected %q in view:\n%s", want, v)
		}
	}
}

func TestNaabuConfigTogglesMode(t *testing.T) {
	m := NewConfig()
	m.focusIndex = fieldMode

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("expected no navigation command when toggling mode")
	}
	if m.mode != naabutool.ModeDiscovery {
		t.Fatalf("mode = %q, want discovery", m.mode)
	}
	if !strings.Contains(m.View(), "[x] discovery") {
		t.Fatal("expected discovery mode in view")
	}
	if !strings.Contains(m.View(), "ignored in discovery") {
		t.Fatal("expected discovery nmap options hint")
	}
}

func TestNaabuConfigCombinedFileRead(t *testing.T) {
	m := NewConfig()
	m.inputs[inputNmapOptions].SetValue("-Pn")
	m.inputs[inputComputeMode].SetValue("auto")
	m.inputs[inputCloud].SetValue("aws")

	_, cmd := m.Update(targetListMsg("/tmp/targets.txt", 2))
	if cmd == nil {
		t.Fatal("expected navigation command after target metadata read")
	}
	msg := cmd()
	nav, ok := msg.(core.NavigateWithDataMsg)
	if !ok {
		t.Fatalf("expected NavigateWithDataMsg, got %T", msg)
	}
	if nav.Target != core.ViewDeploy {
		t.Fatalf("expected ViewDeploy, got %v", nav.Target)
	}
	cfg, ok := nav.Data.(core.DeployConfig)
	if !ok {
		t.Fatalf("expected DeployConfig, got %T", nav.Data)
	}
	if cfg.ToolName != naabutool.ModuleNaabuNmap {
		t.Fatalf("ToolName = %q, want %q", cfg.ToolName, naabutool.ModuleNaabuNmap)
	}
	if cfg.ToolOptions != "-Pn" {
		t.Fatalf("ToolOptions = %q, want -Pn", cfg.ToolOptions)
	}
	if cfg.PostDeployView != core.ViewGenericStatus {
		t.Fatal("expected PostDeployView to be ViewGenericStatus")
	}
	if cfg.TargetsPath != "/tmp/targets.txt" || cfg.TargetCount != 2 {
		t.Fatalf("target metadata = path %q count %d", cfg.TargetsPath, cfg.TargetCount)
	}
	if cfg.BuildArgs == nil {
		t.Fatal("expected BuildArgs to be set")
	}
}

func TestNaabuConfigDiscoveryFileRead(t *testing.T) {
	m := NewConfig()
	m.mode = naabutool.ModeDiscovery
	m.inputs[inputNmapOptions].SetValue("-Pn")
	m.inputs[inputComputeMode].SetValue("auto")
	m.inputs[inputCloud].SetValue("aws")

	_, cmd := m.Update(targetListMsg("/tmp/targets.txt", 1))
	if cmd == nil {
		t.Fatal("expected navigation command after target metadata read")
	}
	msg := cmd()
	nav, ok := msg.(core.NavigateWithDataMsg)
	if !ok {
		t.Fatalf("expected NavigateWithDataMsg, got %T", msg)
	}
	cfg, ok := nav.Data.(core.DeployConfig)
	if !ok {
		t.Fatalf("expected DeployConfig, got %T", nav.Data)
	}
	if cfg.ToolName != naabutool.ModuleNaabu {
		t.Fatalf("ToolName = %q, want %q", cfg.ToolName, naabutool.ModuleNaabu)
	}
	if cfg.ToolOptions != "" {
		t.Fatalf("ToolOptions = %q, want empty discovery options", cfg.ToolOptions)
	}
}

func TestNaabuConfigManualNavigatesToGenericStatus(t *testing.T) {
	t.Setenv("SELFHOSTED_QUEUE_ID", "test-stream")
	t.Setenv("SELFHOSTED_BUCKET", "test-bucket")
	t.Setenv("SELFHOSTED_WORKER_HOSTS", "10.0.0.1")
	t.Setenv("SELFHOSTED_SSH_USER", "heph")
	t.Setenv("SELFHOSTED_DOCKER_IMAGE", "worker:latest")

	m := NewConfig()
	m.inputs[inputComputeMode].SetValue("auto")
	m.inputs[inputCloud].SetValue("manual")
	_, cmd := m.Update(targetListMsg("/tmp/targets.txt", 1))
	if cmd == nil {
		t.Fatal("expected navigation command for manual provider")
	}
	msg := cmd()
	nav, ok := msg.(core.NavigateWithDataMsg)
	if !ok {
		t.Fatalf("expected NavigateWithDataMsg, got %T", msg)
	}
	if nav.Target != core.ViewGenericStatus {
		t.Fatalf("expected ViewGenericStatus, got %v", nav.Target)
	}
	infra, ok := nav.Data.(core.InfraOutputs)
	if !ok {
		t.Fatalf("expected InfraOutputs, got %T", nav.Data)
	}
	if infra.Cloud != cloud.KindManual {
		t.Fatalf("Cloud = %q, want manual", infra.Cloud)
	}
	if infra.ToolName != naabutool.ModuleNaabuNmap {
		t.Fatalf("ToolName = %q, want %q", infra.ToolName, naabutool.ModuleNaabuNmap)
	}
}

func TestNaabuConfigInvalidComputeMode(t *testing.T) {
	m := NewConfig()
	m.inputs[inputCloud].SetValue("manual")
	m.inputs[inputComputeMode].SetValue("fargate")

	_, cmd := m.Update(targetListMsg("/tmp/targets.txt", 1))
	if cmd != nil {
		t.Fatal("expected nil command for manual + fargate")
	}
	if !strings.Contains(m.View(), `provider "manual" only supports`) {
		t.Fatal("expected provider mode rejection error")
	}
}

func TestNaabuConfigSubmitBadFileShowsError(t *testing.T) {
	m := NewConfig()
	m.inputs[inputTargetFile].SetValue("/nonexistent/file.txt")
	m.focusIndex = fieldSubmit

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected command for file read")
	}
	msg := cmd()
	readMsg, ok := msg.(targetListReadMsg)
	if !ok {
		t.Fatalf("expected targetListReadMsg, got %T", msg)
	}
	if readMsg.err == nil {
		t.Fatal("expected error for nonexistent file")
	}

	m.Update(readMsg)
	if m.errMsg == "" {
		t.Fatal("expected error message in view")
	}
}
