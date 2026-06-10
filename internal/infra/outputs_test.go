package infra

import (
	"reflect"
	"testing"

	"heph4estus/internal/cloud"
)

func TestDecodeTerraformOutputs_AWS(t *testing.T) {
	raw := map[string]string{
		OutputToolName:          "nmap",
		OutputSQSQueueURL:       "https://sqs.example.com/q",
		OutputECRRepoURL:        "123.dkr.ecr.us-east-1.amazonaws.com/nmap",
		OutputImageTag:          "heph-nmap-worker-20260608T032422Z-a1b2c3d4",
		OutputDockerImage:       "123.dkr.ecr.us-east-1.amazonaws.com/nmap:tag",
		OutputS3BucketName:      "bucket",
		OutputECSClusterName:    "cluster",
		OutputTaskDefinitionARN: "arn:aws:ecs:task-definition/td",
		OutputSubnetIDs:         "[subnet-a subnet-b]",
		OutputSecurityGroupID:   "sg-123",
		OutputInstanceProfile:   "arn:aws:iam::role",
		OutputAMIID:             "ami-123",
	}

	decoded := DecodeTerraformOutputs(cloud.KindAWS, raw)
	if decoded.ToolName != "nmap" {
		t.Fatalf("ToolName = %q, want nmap", decoded.ToolName)
	}
	if decoded.Cloud != cloud.KindAWS {
		t.Fatalf("Cloud = %q, want aws", decoded.Cloud)
	}
	if decoded.AWS.SQSQueueURL != raw[OutputSQSQueueURL] {
		t.Fatalf("SQSQueueURL = %q", decoded.AWS.SQSQueueURL)
	}
	if !reflect.DeepEqual(decoded.AWS.SubnetIDs, []string{"subnet-a", "subnet-b"}) {
		t.Fatalf("SubnetIDs = %#v", decoded.AWS.SubnetIDs)
	}
	if decoded.AWS.InstanceProfileARN != raw[OutputInstanceProfile] || decoded.AWS.AMIID != raw[OutputAMIID] {
		t.Fatalf("spot outputs = %q/%q", decoded.AWS.InstanceProfileARN, decoded.AWS.AMIID)
	}
}

func TestDecodeTerraformOutputs_ProviderNative(t *testing.T) {
	raw := map[string]string{
		OutputToolName:                     "httpx",
		OutputCloud:                        "hetzner",
		OutputControllerSecurityMode:       "private-auth",
		OutputCredentialScopeVersion:       "v1",
		OutputNATSURL:                      "nats://ctrl:4222",
		OutputNATSStream:                   "heph-tasks",
		OutputNATSTLSEnabled:               "true",
		OutputNATSMTLSEnabled:              "1",
		OutputNATSAuthEnabled:              "yes",
		OutputNATSOperatorClientCertPEM:    "operator-cert",
		OutputNATSOperatorClientKeyPEM:     "operator-key",
		OutputS3Endpoint:                   "https://ctrl:9000",
		OutputS3Region:                     "us-east-1",
		OutputS3AccessKey:                  "ak",
		OutputS3SecretKey:                  "sk",
		OutputS3PathStyle:                  "on",
		OutputS3BucketName:                 "heph-results",
		OutputMinIOTLSEnabled:              "true",
		OutputMinIOAuthEnabled:             "y",
		OutputRegistryURL:                  "https://registry.example.com",
		OutputRegistryTLSEnabled:           "true",
		OutputRegistryAuthEnabled:          "true",
		OutputControllerCAPEM:              "ca",
		OutputControllerHost:               "controller.heph.local",
		OutputControllerIP:                 "203.0.113.10",
		OutputGenerationID:                 "gen-1",
		OutputDockerImage:                  "heph-httpx-worker:latest",
		OutputSQSQueueURL:                  "heph-tasks",
		OutputWorkerCount:                  "3",
		OutputWorkerHosts:                  "203.0.113.11, 203.0.113.12",
		OutputNATSWorkerClientCertNotAfter: "2026-07-01T00:00:00Z",
	}

	decoded := DecodeTerraformOutputs(cloud.KindHetzner, raw)
	out := decoded.Selfhosted
	if decoded.ToolName != "httpx" || decoded.Cloud != cloud.KindHetzner || out.Cloud != cloud.KindHetzner {
		t.Fatalf("decoded identity = tool:%q cloud:%q runtime:%q", decoded.ToolName, decoded.Cloud, out.Cloud)
	}
	if !out.NATSTLSEnabled || !out.NATSMTLSEnabled || !out.NATSAuthEnabled {
		t.Fatalf("expected NATS TLS/mTLS/auth true, got tls=%t mtls=%t auth=%t", out.NATSTLSEnabled, out.NATSMTLSEnabled, out.NATSAuthEnabled)
	}
	if !out.S3PathStyle || !out.MinIOTLSEnabled || !out.MinIOAuthEnabled || !out.RegistryTLSEnabled || !out.RegistryAuthEnabled {
		t.Fatalf("expected storage/registry booleans true")
	}
	if out.WorkerCount != 3 {
		t.Fatalf("WorkerCount = %d, want 3", out.WorkerCount)
	}
	if !reflect.DeepEqual(out.WorkerHosts, []string{"203.0.113.11", "203.0.113.12"}) {
		t.Fatalf("WorkerHosts = %#v", out.WorkerHosts)
	}
	if out.NATSOperatorClientCertPEM != "operator-cert" || out.NATSOperatorClientKeyPEM != "operator-key" {
		t.Fatalf("operator client identity = %q/%q", out.NATSOperatorClientCertPEM, out.NATSOperatorClientKeyPEM)
	}
}

func TestTerraformOutputsMapsAreCopies(t *testing.T) {
	raw := map[string]string{
		OutputToolName:     "nmap",
		OutputS3SecretKey:  "secret",
		OutputS3BucketName: "bucket",
	}
	decoded := DecodeTerraformOutputs(cloud.KindAWS, raw)
	raw[OutputToolName] = "mutated"

	if decoded.ToolName != "nmap" {
		t.Fatalf("ToolName mutated to %q", decoded.ToolName)
	}
	out := decoded.ToMap()
	out[OutputToolName] = "changed"
	if decoded.Raw[OutputToolName] != "nmap" {
		t.Fatalf("ToMap should return a copy")
	}
	redacted := decoded.RedactedMap()
	if redacted[OutputS3SecretKey] != redactedPlaceholder {
		t.Fatalf("expected secret output redacted, got %q", redacted[OutputS3SecretKey])
	}
	if redacted[OutputS3BucketName] != "bucket" {
		t.Fatalf("safe output = %q, want bucket", redacted[OutputS3BucketName])
	}
}

func TestOutputParsers(t *testing.T) {
	outputs := map[string]string{
		"truthy": "ON",
		"count":  "5",
		"spaces": "[subnet-a subnet-b]",
		"commas": "host-a, host-b",
	}

	if !OutputBool(outputs, "truthy") {
		t.Fatal("expected truthy bool")
	}
	if OutputInt(outputs, "count") != 5 {
		t.Fatalf("OutputInt = %d, want 5", OutputInt(outputs, "count"))
	}
	if !reflect.DeepEqual(OutputList(outputs, "spaces"), []string{"subnet-a", "subnet-b"}) {
		t.Fatalf("space list = %#v", OutputList(outputs, "spaces"))
	}
	if !reflect.DeepEqual(OutputList(outputs, "commas"), []string{"host-a", "host-b"}) {
		t.Fatalf("comma list = %#v", OutputList(outputs, "commas"))
	}
	if OutputInt(outputs, "missing") != 0 || OutputBool(outputs, "missing") || OutputString(outputs, "missing") != "" || OutputList(outputs, "missing") != nil {
		t.Fatal("missing outputs should decode to zero values")
	}
}
