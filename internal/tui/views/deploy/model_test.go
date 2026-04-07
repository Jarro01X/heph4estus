package deploy

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"heph4estus/internal/tui/core"
)

// mockDeployer records calls and returns configured results.
type mockDeployer struct {
	initErr       error
	planSummary   string
	planErr       error
	applyErr      error
	readOutputs   map[string]string
	readOutputErr error
	buildErr      error
	ecrAuthErr    error
	tagErr        error
	pushErr       error
}

func (d *mockDeployer) TerraformInit(context.Context, string) error {
	return d.initErr
}

func (d *mockDeployer) TerraformPlan(_ context.Context, _ string, _ map[string]string) (string, error) {
	return d.planSummary, d.planErr
}

func (d *mockDeployer) TerraformApply(_ context.Context, _ string, _ map[string]string, _ io.Writer) error {
	return d.applyErr
}

func (d *mockDeployer) TerraformReadOutputs(context.Context, string) (map[string]string, error) {
	return d.readOutputs, d.readOutputErr
}

func (d *mockDeployer) DockerBuild(_ context.Context, _, _, _ string, _ io.Writer) error {
	return d.buildErr
}

func (d *mockDeployer) DockerBuildWithArgs(_ context.Context, _, _, _ string, _ map[string]string, _ io.Writer) error {
	return d.buildErr
}

func (d *mockDeployer) ECRAuthenticate(context.Context, string) error {
	return d.ecrAuthErr
}

func (d *mockDeployer) DockerTag(_ context.Context, _, _ string) error {
	return d.tagErr
}

func (d *mockDeployer) DockerPush(_ context.Context, _ string, _ io.Writer) error {
	return d.pushErr
}

func (d *mockDeployer) TerraformDestroy(_ context.Context, _ string, _ io.Writer) error {
	return nil
}

// simulateLifecycleDeploy sends a LifecycleCheckMsg indicating deploy is needed,
// bypassing the real terraform probe that Init() would call.
func simulateLifecycleDeploy(m *Model) (core.View, tea.Cmd) {
	return m.Update(core.LifecycleCheckMsg{
		Decision: "deploy",
		Reason:   "test: deploying",
	})
}

// simulateLifecycleReuse sends a LifecycleCheckMsg indicating reuse with outputs.
func simulateLifecycleReuse(m *Model, outputs map[string]string) (core.View, tea.Cmd) {
	return m.Update(core.LifecycleCheckMsg{
		Decision: "reuse",
		Reason:   "test: reusing",
		Outputs:  outputs,
	})
}

func TestDeployModel_InitStartsLifecycleCheck(t *testing.T) {
	m := NewWithDeployer(core.DeployConfig{}, &mockDeployer{})
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("expected init command")
	}
	if m.stage != stageLifecycleCheck {
		t.Fatalf("expected stage %s, got %s", stageLifecycleCheck, m.stage)
	}
}

func TestDeployModel_LifecycleReuse(t *testing.T) {
	outputs := map[string]string{
		"sqs_queue_url":       "https://sqs.example.com/q",
		"ecr_repo_url":        "123.dkr.ecr.us-east-1.amazonaws.com/nmap",
		"s3_bucket_name":      "results-bucket",
		"ecs_cluster_name":    "nmap-cluster",
		"task_definition_arn": "arn:aws:ecs:td",
		"subnet_ids":          "[subnet-a subnet-b]",
		"security_group_id":   "sg-123",
	}

	cfg := core.DeployConfig{
		TargetsContent: "1.1.1.1\n",
		WorkerCount:    5,
	}
	m := NewWithDeployer(cfg, &mockDeployer{})

	// Simulate lifecycle reuse.
	_, cmd := simulateLifecycleReuse(m, outputs)

	if m.stage != stageComplete {
		t.Fatalf("expected stageComplete, got %s", m.stage)
	}
	if !m.lifecycleReuse {
		t.Fatal("expected lifecycleReuse to be true")
	}

	// Should emit navigate.
	if cmd != nil {
		msg := cmd()
		nav, ok := msg.(core.NavigateWithDataMsg)
		if !ok {
			t.Fatalf("expected NavigateWithDataMsg, got %T", msg)
		}
		if nav.Target != core.ViewNmapStatus {
			t.Fatalf("expected ViewNmapStatus, got %v", nav.Target)
		}
	}
}

func TestDeployModel_LifecycleBlock(t *testing.T) {
	m := NewWithDeployer(core.DeployConfig{}, &mockDeployer{})

	m.Update(core.LifecycleCheckMsg{
		Decision: "block",
		Reason:   "terraform is broken",
		Err:      errors.New("broken"),
	})

	if m.stage != stageFailed {
		t.Fatalf("expected stageFailed, got %s", m.stage)
	}
	if !strings.Contains(m.errMsg, "terraform is broken") {
		t.Fatalf("expected error message, got %q", m.errMsg)
	}
}

func TestDeployModel_InitFailure(t *testing.T) {
	m := NewWithDeployer(core.DeployConfig{}, &mockDeployer{
		initErr: errors.New("init failed"),
	})

	// Simulate lifecycle deciding to deploy.
	_, cmd := simulateLifecycleDeploy(m)

	// Run init stage.
	msg := cmd()
	m.Update(msg)

	if m.stage != stageFailed {
		t.Fatalf("expected stageFailed, got %s", m.stage)
	}
	if !strings.Contains(m.errMsg, "init failed") {
		t.Fatalf("expected error msg, got %q", m.errMsg)
	}
}

func TestDeployModel_FullPipeline(t *testing.T) {
	d := &mockDeployer{
		planSummary: "Plan: 5 to add, 0 to change, 0 to destroy.",
		readOutputs: map[string]string{
			"sqs_queue_url":       "https://sqs.example.com/q",
			"ecr_repo_url":        "123.dkr.ecr.us-east-1.amazonaws.com/nmap",
			"s3_bucket_name":      "results-bucket",
			"ecs_cluster_name":    "nmap-cluster",
			"task_definition_arn": "arn:aws:ecs:td",
			"subnet_ids":          "[subnet-a subnet-b]",
			"security_group_id":   "sg-123",
		},
	}
	cfg := core.DeployConfig{
		TerraformDir:   "/tmp/tf",
		DockerTag:      "nmap:latest",
		TargetsContent: "1.1.1.1\n",
		NmapOptions:    "-sS",
		WorkerCount:    5,
	}
	m := NewWithDeployer(cfg, d)

	// Simulate lifecycle deciding to deploy.
	_, cmd := simulateLifecycleDeploy(m)

	// Run init → terraform init
	msg := cmd()
	_, cmd = m.Update(msg) // init complete → plan

	// Plan
	msg = cmd()
	_, _ = m.Update(msg) // plan complete → await approval
	if m.stage != stageAwaitApproval {
		t.Fatalf("expected await-approval, got %s", m.stage)
	}

	// Approve
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'y'})
	// Apply runs with tick; get the StageCompleteMsg
	msgs := drainBatch(cmd)
	for _, msg := range msgs {
		if sc, ok := msg.(core.StageCompleteMsg); ok {
			_, cmd = m.Update(sc) // apply complete → read outputs
			break
		}
	}

	// Read outputs
	if cmd != nil {
		msg = cmd()
		_, cmd = m.Update(msg)
	}

	// Docker build
	if cmd != nil {
		msgs = drainBatch(cmd)
		for _, msg := range msgs {
			if sc, ok := msg.(core.StageCompleteMsg); ok {
				_, cmd = m.Update(sc)
				break
			}
		}
	}

	// ECR auth
	if cmd != nil {
		msg = cmd()
		_, cmd = m.Update(msg)
	}

	// Docker tag+push
	if cmd != nil {
		msgs = drainBatch(cmd)
		for _, msg := range msgs {
			if sc, ok := msg.(core.StageCompleteMsg); ok {
				_, cmd = m.Update(sc)
				break
			}
		}
	}

	if m.stage != stageComplete {
		t.Fatalf("expected stageComplete, got %s", m.stage)
	}

	// Should emit navigate
	if cmd != nil {
		msg = cmd()
		nav, ok := msg.(core.NavigateWithDataMsg)
		if !ok {
			t.Fatalf("expected NavigateWithDataMsg, got %T", msg)
		}
		if nav.Target != core.ViewNmapStatus {
			t.Fatalf("expected ViewNmapStatus, got %v", nav.Target)
		}
		infraOut, ok := nav.Data.(core.InfraOutputs)
		if !ok {
			t.Fatalf("expected InfraOutputs, got %T", nav.Data)
		}
		if infraOut.WorkerCount != 5 {
			t.Fatalf("expected 5 workers, got %d", infraOut.WorkerCount)
		}
	}
}

func TestDeployModel_RejectPlan(t *testing.T) {
	d := &mockDeployer{planSummary: "Plan: 5 to add"}
	m := NewWithDeployer(core.DeployConfig{}, d)

	// Simulate lifecycle deciding to deploy.
	_, cmd := simulateLifecycleDeploy(m)

	msg := cmd()
	_, cmd = m.Update(msg) // init done
	msg = cmd()
	m.Update(msg) // plan done → await approval

	// Reject
	m.Update(tea.KeyPressMsg{Code: 'n'})
	if m.stage != stageRejected {
		t.Fatalf("expected stageRejected, got %s", m.stage)
	}
}

func TestDeployModel_ViewContainsTitle(t *testing.T) {
	m := NewWithDeployer(core.DeployConfig{}, &mockDeployer{})
	v := m.View()
	if !strings.Contains(v, "Deploy Infrastructure") {
		t.Fatal("expected title in view")
	}
}

func TestDeployModel_GenericPostDeployNavigation(t *testing.T) {
	d := &mockDeployer{
		planSummary: "Plan: 1 to add",
		readOutputs: map[string]string{
			"sqs_queue_url":       "https://sqs.example.com/q",
			"ecr_repo_url":        "123.dkr.ecr.us-east-1.amazonaws.com/httpx",
			"s3_bucket_name":      "results-bucket",
			"ecs_cluster_name":    "cluster",
			"task_definition_arn": "arn:aws:ecs:td",
			"subnet_ids":          "[subnet-a]",
			"security_group_id":   "sg-123",
		},
	}
	cfg := core.DeployConfig{
		TerraformDir:   "/tmp/tf",
		DockerTag:      "heph-httpx-worker:latest",
		TargetsContent: "example.com\n",
		WorkerCount:    3,
		ToolName:       "httpx",
		PostDeployView: core.ViewGenericStatus,
	}
	m := NewWithDeployer(cfg, d)

	// Simulate lifecycle deciding to deploy.
	_, cmd := simulateLifecycleDeploy(m)

	// Run the full pipeline.
	msg := cmd()
	_, cmd = m.Update(msg)   // init complete -> plan
	msg = cmd()
	_, _ = m.Update(msg)     // plan complete -> approval
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'y'}) // approve
	msgs := drainBatch(cmd)
	for _, msg := range msgs {
		if sc, ok := msg.(core.StageCompleteMsg); ok {
			_, cmd = m.Update(sc)
			break
		}
	}
	// Read outputs
	if cmd != nil { msg = cmd(); _, cmd = m.Update(msg) }
	// Docker build
	if cmd != nil { msgs = drainBatch(cmd); for _, msg := range msgs { if sc, ok := msg.(core.StageCompleteMsg); ok { _, cmd = m.Update(sc); break } } }
	// ECR auth
	if cmd != nil { msg = cmd(); _, cmd = m.Update(msg) }
	// Docker tag+push
	if cmd != nil { msgs = drainBatch(cmd); for _, msg := range msgs { if sc, ok := msg.(core.StageCompleteMsg); ok { _, cmd = m.Update(sc); break } } }

	if m.stage != stageComplete {
		t.Fatalf("expected stageComplete, got %s", m.stage)
	}

	if cmd != nil {
		msg = cmd()
		nav, ok := msg.(core.NavigateWithDataMsg)
		if !ok {
			t.Fatalf("expected NavigateWithDataMsg, got %T", msg)
		}
		if nav.Target != core.ViewGenericStatus {
			t.Fatalf("expected ViewGenericStatus, got %v", nav.Target)
		}
		infraOut, ok := nav.Data.(core.InfraOutputs)
		if !ok {
			t.Fatalf("expected InfraOutputs, got %T", nav.Data)
		}
		if infraOut.ToolName != "httpx" {
			t.Fatalf("expected tool httpx, got %q", infraOut.ToolName)
		}
	}
}

func TestDeployModel_GenericFailureEscReturnsToGenericConfig(t *testing.T) {
	m := NewWithDeployer(core.DeployConfig{
		ToolName:       "ffuf",
		PostDeployView: core.ViewGenericStatus,
	}, &mockDeployer{
		initErr: errors.New("init failed"),
	})

	// Simulate lifecycle deciding to deploy.
	_, cmd := simulateLifecycleDeploy(m)

	msg := cmd()
	m.Update(msg)

	if m.stage != stageFailed {
		t.Fatalf("expected stageFailed, got %s", m.stage)
	}

	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("expected navigate command on esc")
	}
	msg = cmd()
	nav, ok := msg.(core.NavigateWithDataMsg)
	if !ok {
		t.Fatalf("expected NavigateWithDataMsg, got %T", msg)
	}
	if nav.Target != core.ViewGenericConfig {
		t.Fatalf("expected ViewGenericConfig, got %v", nav.Target)
	}
	if nav.Data != "ffuf" {
		t.Fatalf("expected tool name ffuf, got %#v", nav.Data)
	}
}

func TestDeployModel_GenericRejectEscReturnsToGenericConfig(t *testing.T) {
	m := NewWithDeployer(core.DeployConfig{
		ToolName:       "gobuster",
		PostDeployView: core.ViewGenericStatus,
	}, &mockDeployer{
		planSummary: "Plan: 1 to add",
	})

	// Simulate lifecycle deciding to deploy.
	_, cmd := simulateLifecycleDeploy(m)

	msg := cmd()
	_, cmd = m.Update(msg) // init done
	msg = cmd()
	m.Update(msg) // plan done -> await approval

	m.Update(tea.KeyPressMsg{Code: 'n'})
	if m.stage != stageRejected {
		t.Fatalf("expected stageRejected, got %s", m.stage)
	}

	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("expected navigate command on esc")
	}
	msg = cmd()
	nav, ok := msg.(core.NavigateWithDataMsg)
	if !ok {
		t.Fatalf("expected NavigateWithDataMsg, got %T", msg)
	}
	if nav.Target != core.ViewGenericConfig {
		t.Fatalf("expected ViewGenericConfig, got %v", nav.Target)
	}
	if nav.Data != "gobuster" {
		t.Fatalf("expected tool name gobuster, got %#v", nav.Data)
	}
}

func TestDeployModel_LifecycleReuseShowsCorrectView(t *testing.T) {
	m := NewWithDeployer(core.DeployConfig{}, &mockDeployer{})

	// Simulate reuse.
	simulateLifecycleReuse(m, map[string]string{
		"sqs_queue_url":  "url",
		"s3_bucket_name": "bucket",
	})

	v := m.View()
	if !strings.Contains(v, "Reusing existing infrastructure") {
		t.Fatal("expected reuse message in view")
	}
}

// drainBatch executes a batch command and returns all messages.
func drainBatch(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var msgs []tea.Msg
		for _, c := range batch {
			msgs = append(msgs, c())
		}
		return msgs
	}
	return []tea.Msg{msg}
}
