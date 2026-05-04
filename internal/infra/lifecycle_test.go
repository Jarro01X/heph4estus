package infra

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"heph4estus/internal/cloud"
)

// fullOutputs returns a complete set of terraform outputs for testing.
func fullOutputs(tool string) map[string]string {
	return map[string]string{
		"tool_name":            tool,
		"sqs_queue_url":        "https://sqs.example.com/q",
		"s3_bucket_name":       "results-bucket",
		"ecr_repo_url":         "123.dkr.ecr.us-east-1.amazonaws.com/" + tool,
		"ecs_cluster_name":     "cluster",
		"task_definition_arn":  "arn:aws:ecs:td",
		"subnet_ids":           "[subnet-a subnet-b]",
		"security_group_id":    "sg-123",
		"ami_id":               "ami-123",
		"instance_profile_arn": "arn:aws:iam::role",
	}
}

func TestProbe_Ready(t *testing.T) {
	outputJSON := `{
		"tool_name":{"value":"nmap"},
		"sqs_queue_url":{"value":"https://sqs.example.com/q"},
		"s3_bucket_name":{"value":"bucket"},
		"ecr_repo_url":{"value":"123.dkr.ecr.us-east-1.amazonaws.com/nmap"},
		"ecs_cluster_name":{"value":"cluster"},
		"task_definition_arn":{"value":"arn:aws:ecs:td"},
		"subnet_ids":{"value":"[subnet-a]"},
		"security_group_id":{"value":"sg-123"},
		"ami_id":{"value":"ami-123"},
		"instance_profile_arn":{"value":"arn:aws:iam::role"}
	}`
	tc := &TerraformClient{
		runCmd: newMockExecutor(outputJSON, "", 0, nil),
		logger: nopLogger{},
	}

	result := Probe(context.Background(), tc, cloud.KindAWS, "/work", "nmap")
	if result.Status != StatusReady {
		t.Fatalf("expected StatusReady, got %s", result.Status)
	}
	if result.DeployedTool != "nmap" {
		t.Fatalf("expected deployed tool nmap, got %q", result.DeployedTool)
	}
}

func TestProbe_Missing(t *testing.T) {
	tc := &TerraformClient{
		runCmd: newMockExecutor("{}", "", 0, nil),
		logger: nopLogger{},
	}

	result := Probe(context.Background(), tc, cloud.KindAWS, "/work", "nmap")
	if result.Status != StatusMissing {
		t.Fatalf("expected StatusMissing, got %s", result.Status)
	}
}

func TestProbe_Mismatch(t *testing.T) {
	outputJSON := `{
		"tool_name":{"value":"httpx"},
		"sqs_queue_url":{"value":"https://sqs.example.com/q"},
		"s3_bucket_name":{"value":"bucket"},
		"ecr_repo_url":{"value":"123.dkr.ecr.us-east-1.amazonaws.com/httpx"},
		"ecs_cluster_name":{"value":"cluster"},
		"task_definition_arn":{"value":"arn:aws:ecs:td"},
		"subnet_ids":{"value":"[subnet-a]"},
		"security_group_id":{"value":"sg-123"},
		"ami_id":{"value":"ami-123"},
		"instance_profile_arn":{"value":"arn:aws:iam::role"}
	}`
	tc := &TerraformClient{
		runCmd: newMockExecutor(outputJSON, "", 0, nil),
		logger: nopLogger{},
	}

	result := Probe(context.Background(), tc, cloud.KindAWS, "/work", "nmap")
	if result.Status != StatusMismatch {
		t.Fatalf("expected StatusMismatch, got %s", result.Status)
	}
	if result.DeployedTool != "httpx" {
		t.Fatalf("expected deployed tool httpx, got %q", result.DeployedTool)
	}
}

func TestProbe_Stale(t *testing.T) {
	outputJSON := `{
		"tool_name":{"value":"nmap"},
		"sqs_queue_url":{"value":"https://sqs.example.com/q"}
	}`
	tc := &TerraformClient{
		runCmd: newMockExecutor(outputJSON, "", 0, nil),
		logger: nopLogger{},
	}

	result := Probe(context.Background(), tc, cloud.KindAWS, "/work", "nmap")
	if result.Status != StatusStale {
		t.Fatalf("expected StatusStale, got %s", result.Status)
	}
	if len(result.MissingKeys) == 0 {
		t.Fatal("expected missing keys")
	}
}

func TestProbe_LegacyNoToolName(t *testing.T) {
	// Legacy deployment with no tool_name — should be classified as stale.
	outputJSON := `{
		"sqs_queue_url":{"value":"https://sqs.example.com/q"},
		"s3_bucket_name":{"value":"bucket"},
		"ecr_repo_url":{"value":"123.dkr.ecr.us-east-1.amazonaws.com/old"},
		"ecs_cluster_name":{"value":"cluster"},
		"task_definition_arn":{"value":"arn:aws:ecs:td"},
		"subnet_ids":{"value":"[subnet-a]"},
		"security_group_id":{"value":"sg-123"},
		"ami_id":{"value":"ami-123"},
		"instance_profile_arn":{"value":"arn:aws:iam::role"}
	}`
	tc := &TerraformClient{
		runCmd: newMockExecutor(outputJSON, "", 0, nil),
		logger: nopLogger{},
	}

	result := Probe(context.Background(), tc, cloud.KindAWS, "/work", "nmap")
	if result.Status != StatusStale {
		t.Fatalf("expected StatusStale for legacy state without tool_name, got %s", result.Status)
	}
	found := false
	for _, k := range result.MissingKeys {
		if k == "tool_name" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected tool_name in missing keys, got %v", result.MissingKeys)
	}
}

func TestProbe_MissingSpotOutputs(t *testing.T) {
	// Outputs present but missing ami_id / instance_profile_arn — should be stale.
	outputJSON := `{
		"tool_name":{"value":"nmap"},
		"sqs_queue_url":{"value":"https://sqs.example.com/q"},
		"s3_bucket_name":{"value":"bucket"},
		"ecr_repo_url":{"value":"123.dkr.ecr.us-east-1.amazonaws.com/nmap"},
		"ecs_cluster_name":{"value":"cluster"},
		"task_definition_arn":{"value":"arn:aws:ecs:td"},
		"subnet_ids":{"value":"[subnet-a]"},
		"security_group_id":{"value":"sg-123"}
	}`
	tc := &TerraformClient{
		runCmd: newMockExecutor(outputJSON, "", 0, nil),
		logger: nopLogger{},
	}

	result := Probe(context.Background(), tc, cloud.KindAWS, "/work", "nmap")
	if result.Status != StatusStale {
		t.Fatalf("expected StatusStale for missing spot outputs, got %s", result.Status)
	}
}

func TestProbe_Error(t *testing.T) {
	tc := &TerraformClient{
		runCmd: newMockExecutor("", "terraform error", 1, errors.New("exit 1")),
		logger: nopLogger{},
	}

	result := Probe(context.Background(), tc, cloud.KindAWS, "/work", "nmap")
	if result.Status != StatusError {
		t.Fatalf("expected StatusError, got %s", result.Status)
	}
	if result.Err == nil {
		t.Fatal("expected error to be set")
	}
}

func TestDecide_Reuse(t *testing.T) {
	probe := ProbeResult{
		Status:       StatusReady,
		Outputs:      fullOutputs("nmap"),
		DeployedTool: "nmap",
	}
	result := Decide(probe, LifecyclePolicy{})
	if result.Decision != DecisionReuse {
		t.Fatalf("expected DecisionReuse, got %s", result.Decision)
	}
	if result.Reason != ReasonInfraReady {
		t.Fatalf("expected ReasonInfraReady, got %s", result.Reason)
	}
}

func TestDecide_MissingDeploy(t *testing.T) {
	probe := ProbeResult{Status: StatusMissing}
	result := Decide(probe, LifecyclePolicy{})
	if result.Decision != DecisionDeploy {
		t.Fatalf("expected DecisionDeploy, got %s", result.Decision)
	}
	if result.Reason != ReasonInfraMissing {
		t.Fatalf("expected ReasonInfraMissing, got %s", result.Reason)
	}
}

func TestDecide_MissingNoDeploy(t *testing.T) {
	probe := ProbeResult{Status: StatusMissing}
	result := Decide(probe, LifecyclePolicy{NoDeploy: true})
	if result.Decision != DecisionBlock {
		t.Fatalf("expected DecisionBlock, got %s", result.Decision)
	}
	if result.Reason != ReasonBlockedByPolicy {
		t.Fatalf("expected ReasonBlockedByPolicy, got %s", result.Reason)
	}
}

func TestDecide_MismatchDeploy(t *testing.T) {
	probe := ProbeResult{
		Status:       StatusMismatch,
		Outputs:      fullOutputs("httpx"),
		DeployedTool: "httpx",
	}
	result := Decide(probe, LifecyclePolicy{})
	if result.Decision != DecisionDeploy {
		t.Fatalf("expected DecisionDeploy, got %s", result.Decision)
	}
	if result.Reason != ReasonToolMismatch {
		t.Fatalf("expected ReasonToolMismatch, got %s", result.Reason)
	}
}

func TestDecide_MismatchNoDeploy(t *testing.T) {
	probe := ProbeResult{
		Status:       StatusMismatch,
		Outputs:      fullOutputs("httpx"),
		DeployedTool: "httpx",
	}
	result := Decide(probe, LifecyclePolicy{NoDeploy: true})
	if result.Decision != DecisionBlock {
		t.Fatalf("expected DecisionBlock, got %s", result.Decision)
	}
}

func TestDecide_StaleDeploy(t *testing.T) {
	probe := ProbeResult{
		Status:      StatusStale,
		MissingKeys: []string{"ecr_repo_url"},
	}
	result := Decide(probe, LifecyclePolicy{})
	if result.Decision != DecisionDeploy {
		t.Fatalf("expected DecisionDeploy, got %s", result.Decision)
	}
}

func TestDecide_StaleNoDeploy(t *testing.T) {
	probe := ProbeResult{
		Status:      StatusStale,
		MissingKeys: []string{"ecr_repo_url"},
	}
	result := Decide(probe, LifecyclePolicy{NoDeploy: true})
	if result.Decision != DecisionBlock {
		t.Fatalf("expected DecisionBlock, got %s", result.Decision)
	}
}

func TestDecide_ErrorAlwaysBlocks(t *testing.T) {
	probe := ProbeResult{
		Status: StatusError,
		Err:    errors.New("terraform broken"),
	}
	// Even without NoDeploy, errors should block.
	result := Decide(probe, LifecyclePolicy{})
	if result.Decision != DecisionBlock {
		t.Fatalf("expected DecisionBlock, got %s", result.Decision)
	}
	if result.Reason != ReasonProbeError {
		t.Fatalf("expected ReasonProbeError, got %s", result.Reason)
	}
}

// --- Provider-aware required output tests ---

func TestRequiredOutputKeysForCloud_AWS(t *testing.T) {
	keys := RequiredOutputKeysForCloud(cloud.KindAWS)
	if len(keys) != len(AWSRequiredOutputKeys) {
		t.Fatalf("AWS keys length = %d, want %d", len(keys), len(AWSRequiredOutputKeys))
	}
	// Spot check a few AWS-specific keys.
	want := map[string]bool{
		"sqs_queue_url":        true,
		"ecr_repo_url":         true,
		"ecs_cluster_name":     true,
		"ami_id":               true,
		"instance_profile_arn": true,
	}
	for _, k := range keys {
		delete(want, k)
	}
	if len(want) > 0 {
		t.Fatalf("missing expected AWS keys: %v", want)
	}
}

func TestRequiredOutputKeysForCloud_Selfhosted(t *testing.T) {
	keys := RequiredOutputKeysForCloud(cloud.KindSelfhosted)
	if len(keys) != len(SelfhostedRequiredOutputKeys) {
		t.Fatalf("selfhosted keys length = %d, want %d", len(keys), len(SelfhostedRequiredOutputKeys))
	}
	// Selfhosted should only require tool_name for mismatch detection.
	if keys[0] != "tool_name" {
		t.Fatalf("expected tool_name, got %q", keys[0])
	}
	// AWS-specific keys must NOT appear in selfhosted set.
	for _, k := range keys {
		if k == "sqs_queue_url" || k == "ecr_repo_url" || k == "ecs_cluster_name" {
			t.Fatalf("selfhosted should not require AWS key %q", k)
		}
	}
}

func TestRequiredOutputKeysForCloud_Hetzner(t *testing.T) {
	keys := RequiredOutputKeysForCloud(cloud.KindHetzner)
	if len(keys) != len(HetznerRequiredOutputKeys) {
		t.Fatalf("Hetzner keys length = %d, want %d", len(keys), len(HetznerRequiredOutputKeys))
	}
	want := map[string]bool{
		"controller_security_mode":         true,
		"nats_url":                         true,
		"nats_tls_enabled":                 true,
		"nats_auth_enabled":                true,
		"minio_tls_enabled":                true,
		"minio_auth_enabled":               true,
		"registry_tls_enabled":             true,
		"registry_auth_enabled":            true,
		"controller_ca_pem":                true,
		"controller_ca_fingerprint_sha256": true,
		"controller_cert_not_after":        true,
		"controller_host":                  true,
		"controller_ip":                    true,
		"generation_id":                    true,
		"worker_hosts":                     true,
	}
	for _, k := range keys {
		delete(want, k)
	}
	if len(want) > 0 {
		t.Fatalf("missing expected Hetzner keys: %v", want)
	}
}

func TestRequiredOutputKeysForCloud_Linode(t *testing.T) {
	keys := RequiredOutputKeysForCloud(cloud.KindLinode)
	if len(keys) != len(LinodeRequiredOutputKeys) {
		t.Fatalf("Linode keys length = %d, want %d", len(keys), len(LinodeRequiredOutputKeys))
	}
	want := map[string]bool{
		"controller_security_mode":         true,
		"nats_url":                         true,
		"nats_tls_enabled":                 true,
		"nats_auth_enabled":                true,
		"minio_tls_enabled":                true,
		"minio_auth_enabled":               true,
		"registry_tls_enabled":             true,
		"registry_auth_enabled":            true,
		"controller_ca_pem":                true,
		"controller_ca_fingerprint_sha256": true,
		"controller_cert_not_after":        true,
		"controller_host":                  true,
		"controller_ip":                    true,
		"generation_id":                    true,
		"worker_hosts":                     true,
	}
	for _, k := range keys {
		delete(want, k)
	}
	if len(want) > 0 {
		t.Fatalf("missing expected Linode keys: %v", want)
	}
}

func TestRequiredOutputKeysForCloud_UnknownFallsToAWS(t *testing.T) {
	keys := RequiredOutputKeysForCloud("")
	if len(keys) != len(AWSRequiredOutputKeys) {
		t.Fatalf("empty kind should fall back to AWS, got %d keys", len(keys))
	}
}

func TestProbe_SelfhostedReadyWithToolName(t *testing.T) {
	// Selfhosted only requires tool_name — outputs with just tool_name should be ready.
	outputJSON := `{"tool_name":{"value":"nmap"}}`
	tc := &TerraformClient{
		runCmd: newMockExecutor(outputJSON, "", 0, nil),
		logger: nopLogger{},
	}

	result := Probe(context.Background(), tc, cloud.KindSelfhosted, "/work", "nmap")
	if result.Status != StatusReady {
		t.Fatalf("expected StatusReady for selfhosted with tool_name, got %s (missing: %v)", result.Status, result.MissingKeys)
	}
}

func TestProbe_SelfhostedMissingToolName(t *testing.T) {
	// Selfhosted outputs missing tool_name should be stale.
	outputJSON := `{"some_key":{"value":"val"}}`
	tc := &TerraformClient{
		runCmd: newMockExecutor(outputJSON, "", 0, nil),
		logger: nopLogger{},
	}

	result := Probe(context.Background(), tc, cloud.KindSelfhosted, "/work", "nmap")
	if result.Status != StatusStale {
		t.Fatalf("expected StatusStale for selfhosted without tool_name, got %s", result.Status)
	}
}

// --- Cloud mismatch tests ---

func hetznerOutputs(tool string) string {
	return fmt.Sprintf(`{
		"tool_name":{"value":"%s"},
		"cloud":{"value":"hetzner"},
		"controller_security_mode":{"value":"private-auth"},
		"nats_url":{"value":"nats://heph:secret@10.0.1.2:4222"},
		"nats_stream":{"value":"heph"},
		"nats_user":{"value":"heph"},
		"nats_password":{"value":"secret"},
		"nats_tls_enabled":{"value":"false"},
		"nats_auth_enabled":{"value":"true"},
		"s3_endpoint":{"value":"http://10.0.1.2:9000"},
		"s3_access_key":{"value":"admin"},
		"s3_secret_key":{"value":"secret"},
		"s3_bucket_name":{"value":"heph-results"},
		"minio_tls_enabled":{"value":"false"},
		"minio_auth_enabled":{"value":"true"},
		"registry_url":{"value":"10.0.1.2:5000"},
		"registry_tls_enabled":{"value":"false"},
		"registry_auth_enabled":{"value":"false"},
		"controller_ca_pem":{"value":"-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----"},
		"controller_ca_fingerprint_sha256":{"value":"abc123"},
		"controller_cert_not_after":{"value":"2027-05-03T00:00:00Z"},
		"controller_host":{"value":"heph-controller"},
		"docker_image":{"value":"scanner-nmap:latest"},
		"sqs_queue_url":{"value":"nmap"},
		"controller_ip":{"value":"1.2.3.4"},
		"generation_id":{"value":"gen-abc"},
		"worker_count":{"value":"3"},
		"worker_hosts":{"value":"10.0.1.10,10.0.1.11,10.0.1.12"}
	}`, tool)
}

func TestProbe_CloudMismatch_HetznerToVultr(t *testing.T) {
	// Hetzner outputs should be detected as mismatch when Vultr is requested.
	tc := &TerraformClient{
		runCmd: newMockExecutor(hetznerOutputs("nmap"), "", 0, nil),
		logger: nopLogger{},
	}

	result := Probe(context.Background(), tc, cloud.KindVultr, "/work", "nmap")
	if result.Status != StatusMismatch {
		t.Fatalf("expected StatusMismatch for cloud mismatch (hetzner deployed, vultr requested), got %s", result.Status)
	}
}

func TestProbe_CloudMatch_HetznerToHetzner(t *testing.T) {
	// Hetzner outputs should be ready when Hetzner is requested.
	tc := &TerraformClient{
		runCmd: newMockExecutor(hetznerOutputs("nmap"), "", 0, nil),
		logger: nopLogger{},
	}

	result := Probe(context.Background(), tc, cloud.KindHetzner, "/work", "nmap")
	if result.Status != StatusReady {
		t.Fatalf("expected StatusReady for matching cloud, got %s (missing: %v)", result.Status, result.MissingKeys)
	}
}

func TestProbe_CloudMismatch_IgnoredForAWS(t *testing.T) {
	// AWS probe should not check cloud output — it's only relevant for provider-native.
	outputJSON := `{
		"tool_name":{"value":"nmap"},
		"cloud":{"value":"hetzner"},
		"sqs_queue_url":{"value":"https://sqs.example.com/q"},
		"s3_bucket_name":{"value":"bucket"},
		"ecr_repo_url":{"value":"123.dkr.ecr.us-east-1.amazonaws.com/nmap"},
		"ecs_cluster_name":{"value":"cluster"},
		"task_definition_arn":{"value":"arn:aws:ecs:td"},
		"subnet_ids":{"value":"[subnet-a]"},
		"security_group_id":{"value":"sg-123"},
		"ami_id":{"value":"ami-123"},
		"instance_profile_arn":{"value":"arn:aws:iam::role"}
	}`
	tc := &TerraformClient{
		runCmd: newMockExecutor(outputJSON, "", 0, nil),
		logger: nopLogger{},
	}

	result := Probe(context.Background(), tc, cloud.KindAWS, "/work", "nmap")
	// AWS should not cloud-check — only required keys and tool_name matter.
	if result.Status != StatusReady {
		t.Fatalf("expected StatusReady for AWS (cloud mismatch should be ignored), got %s", result.Status)
	}
}
