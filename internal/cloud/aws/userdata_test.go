package aws

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestGenerateUserData_ContainsExpectedCommands(t *testing.T) {
	ud := GenerateUserData(UserDataOpts{
		ECRRepoURL: "123456789.dkr.ecr.us-east-1.amazonaws.com/nmap-scanner",
		ImageTag:   "latest",
		Region:     "us-east-1",
		EnvVars: map[string]string{
			"QUEUE_URL": "https://sqs/q",
			"S3_BUCKET": "my-bucket",
		},
	})

	decoded, err := base64.StdEncoding.DecodeString(ud)
	if err != nil {
		t.Fatalf("invalid base64: %v", err)
	}
	script := string(decoded)

	checks := []string{
		"#!/bin/bash",
		"set -e",
		"yum install -y docker",
		"docker login",
		"docker pull 123456789.dkr.ecr.us-east-1.amazonaws.com/nmap-scanner:latest",
		"docker run",
		"-e QUEUE_URL=https://sqs/q",
		"-e S3_BUCKET=my-bucket",
		"terminate-instances",
		"169.254.169.254/latest/meta-data/instance-id",
	}

	for _, check := range checks {
		if !strings.Contains(script, check) {
			t.Errorf("expected script to contain %q", check)
		}
	}
}

func TestGenerateUserData_IsValidBase64(t *testing.T) {
	ud := GenerateUserData(UserDataOpts{
		ECRRepoURL: "123.dkr.ecr.us-west-2.amazonaws.com/repo",
		ImageTag:   "v1",
		Region:     "us-west-2",
	})

	_, err := base64.StdEncoding.DecodeString(ud)
	if err != nil {
		t.Fatalf("GenerateUserData did not produce valid base64: %v", err)
	}
}

func TestGenerateUserData_ECRRegistryExtracted(t *testing.T) {
	ud := GenerateUserData(UserDataOpts{
		ECRRepoURL: "999.dkr.ecr.eu-west-1.amazonaws.com/my-image",
		ImageTag:   "latest",
		Region:     "eu-west-1",
	})

	decoded, _ := base64.StdEncoding.DecodeString(ud)
	script := string(decoded)

	if !strings.Contains(script, "999.dkr.ecr.eu-west-1.amazonaws.com") {
		t.Error("expected ECR registry in login command")
	}
}

func TestGenerateUserData_EnvVarsSorted(t *testing.T) {
	ud := GenerateUserData(UserDataOpts{
		ECRRepoURL: "123.dkr.ecr.us-east-1.amazonaws.com/repo",
		ImageTag:   "v1",
		Region:     "us-east-1",
		EnvVars: map[string]string{
			"ZZZ": "last",
			"AAA": "first",
		},
	})

	decoded, _ := base64.StdEncoding.DecodeString(ud)
	script := string(decoded)

	aIdx := strings.Index(script, "-e AAA=first")
	zIdx := strings.Index(script, "-e ZZZ=last")
	if aIdx < 0 || zIdx < 0 {
		t.Fatal("expected both env vars in script")
	}
	if aIdx > zIdx {
		t.Error("expected env vars to be sorted")
	}
}
