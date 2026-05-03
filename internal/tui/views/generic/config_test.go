package generic

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"heph4estus/internal/cloud"
	"heph4estus/internal/tui/core"
)

func TestGenericConfigShowsToolName(t *testing.T) {
	m := NewConfig("httpx")
	v := m.View()
	if !strings.Contains(v, "httpx") {
		t.Fatal("expected config view to contain tool name")
	}
}

func TestGenericConfigShowsDescription(t *testing.T) {
	m := NewConfig("httpx")
	v := m.View()
	if !strings.Contains(v, "HTTP probe") {
		t.Fatal("expected config view to contain module description")
	}
}

func TestGenericConfigEscNavigatesBack(t *testing.T) {
	m := NewConfig("httpx")
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("expected command from esc")
	}
	msg := cmd()
	nav, ok := msg.(core.NavigateMsg)
	if !ok {
		t.Fatalf("expected NavigateMsg, got %T", msg)
	}
	if nav.Target != core.ViewMenu {
		t.Fatalf("expected ViewMenu, got %v", nav.Target)
	}
}

func TestGenericConfigSubmitRequiresTargetFile(t *testing.T) {
	m := NewConfig("httpx")
	// Navigate to submit button.
	for i := 0; i < cfgFieldSubmit; i++ {
		m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	}
	// Press enter on submit.
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("expected nil command when submitting without target file")
	}
	if !strings.Contains(m.View(), "Target file is required") {
		t.Fatal("expected error message about target file")
	}
}

func TestGenericConfigWordlistRequiresFile(t *testing.T) {
	m := NewConfig("ffuf")
	if !m.isWordlist {
		t.Fatal("expected ffuf to be detected as wordlist module")
	}
	// Navigate to submit.
	m.focus = wlFieldSubmit
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("expected nil command when submitting without wordlist file")
	}
	if !strings.Contains(m.View(), "Wordlist file is required") {
		t.Fatal("expected wordlist file required error")
	}
}

func TestGenericConfigWordlistRequiresTarget(t *testing.T) {
	m := NewConfig("ffuf")
	m.wlInputs[wlFieldWordlistFile].SetValue("/tmp/words.txt")
	// Leave target empty — ffuf requires {{target}}.
	m.focus = wlFieldSubmit
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("expected nil command when submitting without target")
	}
	if !strings.Contains(m.View(), "Target / URL is required") {
		t.Fatal("expected target required error")
	}
}

func TestGenericConfigWordlistInvalidChunks(t *testing.T) {
	m := NewConfig("ffuf")
	m.wlInputs[wlFieldWordlistFile].SetValue("/tmp/words.txt")
	m.wlInputs[wlFieldTarget].SetValue("https://example.com/FUZZ")
	m.wlInputs[wlFieldChunks].SetValue("-5")
	m.focus = wlFieldSubmit
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("expected nil command for invalid chunks")
	}
	if !strings.Contains(m.View(), "Chunks must be a positive number") {
		t.Fatal("expected chunks validation error")
	}
}

func TestGenericConfigWordlistShowsFields(t *testing.T) {
	m := NewConfig("ffuf")
	v := m.View()
	if !strings.Contains(v, "Wordlist File") {
		t.Fatal("expected Wordlist File label")
	}
	if !strings.Contains(v, "Target / URL") {
		t.Fatal("expected Target / URL label")
	}
	if !strings.Contains(v, "Chunks") {
		t.Fatal("expected Chunks label")
	}
}

func TestGenericConfigFileRead(t *testing.T) {
	m := NewConfig("httpx")
	m.inputs[cfgFieldComputeMode].SetValue("auto")
	m.inputs[cfgFieldCloud].SetValue("aws")
	// Simulate successful file read.
	_, cmd := m.Update(fileReadMsg{content: "example.com\n10.0.0.1\n"})
	if cmd == nil {
		t.Fatal("expected navigation command after file read")
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
	if cfg.ToolName != "httpx" {
		t.Fatalf("expected tool httpx, got %q", cfg.ToolName)
	}
	if cfg.PostDeployView != core.ViewGenericStatus {
		t.Fatal("expected PostDeployView to be ViewGenericStatus")
	}
	if cfg.TargetsContent != "example.com\n10.0.0.1\n" {
		t.Fatalf("unexpected targets content: %q", cfg.TargetsContent)
	}
	if cfg.BuildArgs == nil {
		t.Fatal("expected BuildArgs to be set")
	}
}

func TestGenericConfigInvalidComputeMode(t *testing.T) {
	m := NewConfig("httpx")
	m.inputs[cfgFieldCloud].SetValue("aws")
	m.inputs[cfgFieldComputeMode].SetValue("gpu")
	_, cmd := m.Update(fileReadMsg{content: "example.com\n"})
	if cmd != nil {
		t.Fatal("expected nil command for invalid compute mode")
	}
	if !strings.Contains(m.View(), "compute-mode must be") {
		t.Fatal("expected compute mode error message")
	}
}

func TestGenericConfigHetznerNavigatesToDeploy(t *testing.T) {
	// Hetzner is provider-native: goes through deploy view, not direct to status.
	t.Setenv("HCLOUD_TOKEN", "hcloud-test")
	t.Setenv("HEPH_SSH_PUBLIC_KEY", "ssh-ed25519 tui-test")
	m := NewConfig("httpx")
	m.inputs[cfgFieldComputeMode].SetValue("auto")
	m.inputs[cfgFieldCloud].SetValue("hetzner")
	_, cmd := m.Update(fileReadMsg{content: "example.com\n"})
	if cmd == nil {
		t.Fatal("expected navigation command for Hetzner provider")
	}
	msg := cmd()
	nav, ok := msg.(core.NavigateWithDataMsg)
	if !ok {
		t.Fatalf("expected NavigateWithDataMsg, got %T", msg)
	}
	if nav.Target != core.ViewDeploy {
		t.Fatalf("expected ViewDeploy for provider-native Hetzner, got %v", nav.Target)
	}
	cfg, ok := nav.Data.(core.DeployConfig)
	if !ok {
		t.Fatalf("expected DeployConfig, got %T", nav.Data)
	}
	if cfg.Cloud != cloud.KindHetzner {
		t.Fatalf("expected cloud hetzner, got %q", cfg.Cloud)
	}
	if cfg.TerraformDir != "deployments/hetzner" {
		t.Fatalf("expected hetzner terraform dir, got %q", cfg.TerraformDir)
	}
}

func TestGenericConfigManualNavigatesToStatus(t *testing.T) {
	t.Setenv("SELFHOSTED_QUEUE_ID", "test-stream")
	t.Setenv("SELFHOSTED_BUCKET", "test-bucket")
	t.Setenv("SELFHOSTED_WORKER_HOSTS", "10.0.0.1")
	t.Setenv("SELFHOSTED_SSH_USER", "heph")
	t.Setenv("SELFHOSTED_DOCKER_IMAGE", "worker:latest")

	m := NewConfig("httpx")
	m.inputs[cfgFieldComputeMode].SetValue("auto")
	m.inputs[cfgFieldCloud].SetValue("manual")
	_, cmd := m.Update(fileReadMsg{content: "example.com\n"})
	if cmd == nil {
		t.Fatal("expected navigation command for manual provider")
	}
	msg := cmd()
	nav, ok := msg.(core.NavigateWithDataMsg)
	if !ok {
		t.Fatalf("expected NavigateWithDataMsg, got %T", msg)
	}
	if nav.Target != core.ViewGenericStatus {
		t.Fatalf("expected ViewGenericStatus (bypass deploy for manual), got %v", nav.Target)
	}
	infra, ok := nav.Data.(core.InfraOutputs)
	if !ok {
		t.Fatalf("expected InfraOutputs, got %T", nav.Data)
	}
	if infra.Cloud != cloud.KindManual {
		t.Fatalf("expected cloud manual, got %q", infra.Cloud)
	}
}

func TestGenericConfigManualRejectsFargate(t *testing.T) {
	m := NewConfig("httpx")
	m.inputs[cfgFieldCloud].SetValue("manual")
	m.inputs[cfgFieldComputeMode].SetValue("fargate")
	_, cmd := m.Update(fileReadMsg{content: "example.com\n"})
	if cmd != nil {
		t.Fatal("expected nil command for manual + fargate")
	}
	if !strings.Contains(m.View(), `provider "manual" only supports`) {
		t.Fatal("expected provider mode rejection error")
	}
}

func TestGenericConfigManualMissingEnv(t *testing.T) {
	t.Setenv("SELFHOSTED_QUEUE_ID", "")
	t.Setenv("SELFHOSTED_BUCKET", "")

	m := NewConfig("httpx")
	m.inputs[cfgFieldComputeMode].SetValue("auto")
	m.inputs[cfgFieldCloud].SetValue("manual")
	_, cmd := m.Update(fileReadMsg{content: "example.com\n"})
	if cmd != nil {
		t.Fatal("expected nil command when manual env is missing")
	}
	if !strings.Contains(m.View(), "manual requires SELFHOSTED_QUEUE_ID") {
		t.Fatal("expected env var requirement error")
	}
}
