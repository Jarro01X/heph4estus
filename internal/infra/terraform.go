package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"

	"heph4estus/internal/logger"
)

// TerraformClient wraps the terraform CLI binary.
type TerraformClient struct {
	runCmd CommandExecutor
	logger logger.Logger
}

// NewTerraformClient creates a TerraformClient using DefaultExecutor.
func NewTerraformClient(logger logger.Logger) *TerraformClient {
	return &TerraformClient{
		runCmd: DefaultExecutor,
		logger: logger,
	}
}

// Init runs terraform init in the given working directory.
func (t *TerraformClient) Init(ctx context.Context, workDir string) error {
	t.logger.Info("Running terraform init in %s", workDir)
	result, err := t.runCmd(ctx, workDir, nil, "terraform", "init", "-input=false")
	if err != nil {
		t.logger.Error("terraform init failed: %s", string(result.Stderr))
		return fmt.Errorf("terraform init: %w", err)
	}
	return nil
}

// Plan runs terraform plan and returns a summary of the planned changes.
func (t *TerraformClient) Plan(ctx context.Context, workDir string, vars map[string]string) (string, error) {
	t.logger.Info("Running terraform plan in %s", workDir)
	args := []string{"terraform", "plan", "-input=false", "-no-color"}
	args = append(args, varFlags(vars)...)

	result, err := t.runCmd(ctx, workDir, nil, args...)
	if err != nil {
		t.logger.Error("terraform plan failed: %s", string(result.Stderr))
		return "", fmt.Errorf("terraform plan: %w", err)
	}
	return parsePlanSummary(string(result.Stdout)), nil
}

// Apply runs terraform apply with auto-approve and streams output.
func (t *TerraformClient) Apply(ctx context.Context, workDir string, vars map[string]string, stream io.Writer) error {
	t.logger.Info("Running terraform apply in %s", workDir)
	args := []string{"terraform", "apply", "-auto-approve", "-input=false", "-no-color"}
	args = append(args, varFlags(vars)...)

	result, err := t.runCmd(ctx, workDir, stream, args...)
	if err != nil {
		t.logger.Error("terraform apply failed: %s", string(result.Stderr))
		return fmt.Errorf("terraform apply: %w", err)
	}
	return nil
}

// ApplyReplace runs terraform apply while forcing replacement of the provided
// resource addresses.
func (t *TerraformClient) ApplyReplace(ctx context.Context, workDir string, vars map[string]string, replaceAddrs []string, stream io.Writer) error {
	t.logger.Info("Running terraform apply -replace in %s", workDir)
	args := []string{"terraform", "apply", "-auto-approve", "-input=false", "-no-color"}
	for _, addr := range replaceAddrs {
		if addr == "" {
			continue
		}
		args = append(args, "-replace="+addr)
	}
	args = append(args, varFlags(vars)...)

	result, err := t.runCmd(ctx, workDir, stream, args...)
	if err != nil {
		t.logger.Error("terraform apply -replace failed: %s", string(result.Stderr))
		return fmt.Errorf("terraform apply -replace: %w", err)
	}
	return nil
}

// ShowJSON runs terraform show -json and returns the raw state document.
func (t *TerraformClient) ShowJSON(ctx context.Context, workDir string) ([]byte, error) {
	t.logger.Info("Reading terraform state JSON in %s", workDir)
	result, err := t.runCmd(ctx, workDir, nil, "terraform", "show", "-json")
	if err != nil {
		t.logger.Error("terraform show -json failed: %s", string(result.Stderr))
		return nil, fmt.Errorf("terraform show -json: %w", err)
	}
	return result.Stdout, nil
}

// Destroy runs terraform destroy with auto-approve and streams output.
func (t *TerraformClient) Destroy(ctx context.Context, workDir string, stream io.Writer) error {
	t.logger.Info("Running terraform destroy in %s", workDir)
	result, err := t.runCmd(ctx, workDir, stream, "terraform", "destroy", "-auto-approve", "-input=false", "-no-color")
	if err != nil {
		t.logger.Error("terraform destroy failed: %s", string(result.Stderr))
		return fmt.Errorf("terraform destroy: %w", err)
	}
	return nil
}

// terraformOutput represents a single output from terraform output -json.
type terraformOutput struct {
	Value interface{} `json:"value"`
}

// ReadOutputs runs terraform output -json and returns the values as a flat string map.
func (t *TerraformClient) ReadOutputs(ctx context.Context, workDir string) (map[string]string, error) {
	t.logger.Info("Reading terraform outputs in %s", workDir)
	result, err := t.runCmd(ctx, workDir, nil, "terraform", "output", "-json")
	if err != nil {
		t.logger.Error("terraform output failed: %s", string(result.Stderr))
		return nil, fmt.Errorf("terraform output: %w", err)
	}

	var raw map[string]terraformOutput
	if err := json.Unmarshal(result.Stdout, &raw); err != nil {
		return nil, fmt.Errorf("parsing terraform output JSON: %w", err)
	}

	outputs := make(map[string]string, len(raw))
	for k, v := range raw {
		outputs[k] = fmt.Sprintf("%v", v.Value)
	}
	return outputs, nil
}

var planSummaryRe = regexp.MustCompile(`Plan: \d+ to add, \d+ to change, \d+ to destroy\.`)

func parsePlanSummary(output string) string {
	if strings.Contains(output, "No changes.") {
		return "No changes."
	}
	if m := planSummaryRe.FindString(output); m != "" {
		return m
	}
	return "Plan completed."
}

func varFlags(vars map[string]string) []string {
	flags := make([]string, 0, len(vars)*2)
	for k, v := range vars {
		flags = append(flags, "-var", k+"="+v)
	}
	return flags
}
