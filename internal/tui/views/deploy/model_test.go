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

func TestDeployModel_InitStartsTerraformInit(t *testing.T) {
	m := NewWithDeployer(core.DeployConfig{}, &mockDeployer{})
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("expected init command")
	}
	if m.stage != stageTerraformInit {
		t.Fatalf("expected stage %s, got %s", stageTerraformInit, m.stage)
	}
}

func TestDeployModel_InitFailure(t *testing.T) {
	m := NewWithDeployer(core.DeployConfig{}, &mockDeployer{
		initErr: errors.New("init failed"),
	})
	cmd := m.Init()
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

	// Run init → terraform init
	cmd := m.Init()
	msg := cmd()
	_, cmd = m.Update(msg) // init complete → plan

	// Plan
	msg = cmd()
	_, cmd = m.Update(msg) // plan complete → await approval
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
		infra, ok := nav.Data.(core.InfraOutputs)
		if !ok {
			t.Fatalf("expected InfraOutputs, got %T", nav.Data)
		}
		if infra.WorkerCount != 5 {
			t.Fatalf("expected 5 workers, got %d", infra.WorkerCount)
		}
	}
}

func TestDeployModel_RejectPlan(t *testing.T) {
	d := &mockDeployer{planSummary: "Plan: 5 to add"}
	m := NewWithDeployer(core.DeployConfig{}, d)

	cmd := m.Init()
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
