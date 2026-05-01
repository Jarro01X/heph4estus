package infra

import (
	"context"
	"errors"
	"io"
	"testing"

	"heph4estus/internal/cloud"
	"heph4estus/internal/logger"
)

func TestWorkerResourceAddress(t *testing.T) {
	tests := []struct {
		kind cloud.Kind
		want string
	}{
		{kind: cloud.KindHetzner, want: "hcloud_server.worker[2]"},
		{kind: cloud.KindLinode, want: "linode_instance.worker[2]"},
		{kind: cloud.KindVultr, want: "vultr_instance.worker[2]"},
	}
	for _, tt := range tests {
		got, err := WorkerResourceAddress(tt.kind, 2)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tt.kind, err)
		}
		if got != tt.want {
			t.Fatalf("%s: got %q, want %q", tt.kind, got, tt.want)
		}
	}
	if _, err := WorkerResourceAddress(cloud.KindAWS, 1); err == nil {
		t.Fatal("expected unsupported provider error")
	}
}

func TestTerraformClientApplyReplaceAddsReplaceFlags(t *testing.T) {
	var gotArgs []string
	client := &TerraformClient{
		logger: logger.NewSimpleLogger(),
		runCmd: func(_ context.Context, _ string, _ io.Writer, args ...string) (*CommandResult, error) {
			gotArgs = append([]string(nil), args...)
			return &CommandResult{}, nil
		},
	}

	if err := client.ApplyReplace(context.Background(), "deployments/hetzner", map[string]string{"docker_image": "registry/heph:2"}, []string{"hcloud_server.worker[0]", "hcloud_server.worker[2]"}, nil); err != nil {
		t.Fatalf("ApplyReplace: %v", err)
	}

	for _, want := range []string{
		"terraform",
		"apply",
		"-replace=hcloud_server.worker[0]",
		"-replace=hcloud_server.worker[2]",
		"-var",
		"docker_image=registry/heph:2",
	} {
		found := false
		for _, arg := range gotArgs {
			if arg == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected %q in args: %#v", want, gotArgs)
		}
	}
}

func TestTerraformClientApplyReplaceWrapsErrors(t *testing.T) {
	client := &TerraformClient{
		logger: logger.NewSimpleLogger(),
		runCmd: func(_ context.Context, _ string, _ io.Writer, _ ...string) (*CommandResult, error) {
			return &CommandResult{}, errors.New("boom")
		},
	}
	if err := client.ApplyReplace(context.Background(), "deployments/vultr", nil, []string{"vultr_instance.worker[0]"}, nil); err == nil {
		t.Fatal("expected error")
	}
}
