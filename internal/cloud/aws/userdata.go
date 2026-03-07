package aws

import (
	"encoding/base64"
	"fmt"
	"sort"
	"strings"
)

// UserDataOpts configures the bootstrap script for spot instances.
type UserDataOpts struct {
	ECRRepoURL string
	ImageTag   string
	Region     string
	EnvVars    map[string]string
}

// GenerateUserData creates a base64-encoded bash script that bootstraps a spot
// instance: installs Docker, pulls the worker image from ECR, runs it, and
// self-terminates when done.
func GenerateUserData(opts UserDataOpts) string {
	imageRef := opts.ECRRepoURL + ":" + opts.ImageTag

	// Build sorted env var flags for deterministic output
	var envFlags []string
	keys := make([]string, 0, len(opts.EnvVars))
	for k := range opts.EnvVars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		envFlags = append(envFlags, fmt.Sprintf("-e %s=%s", k, opts.EnvVars[k]))
	}
	envStr := strings.Join(envFlags, " ")

	// ECR registry is the repo URL up to the first slash
	ecrRegistry := opts.ECRRepoURL
	if idx := strings.Index(ecrRegistry, "/"); idx > 0 {
		ecrRegistry = ecrRegistry[:idx]
	}

	script := fmt.Sprintf(`#!/bin/bash
set -e

# Install and start Docker
yum install -y docker
systemctl start docker
systemctl enable docker

# ECR login
aws ecr get-login-password --region %s | docker login --username AWS --password-stdin %s

# Pull and run the worker image
docker pull %s
docker run --rm %s %s

# Self-terminate after container exits
INSTANCE_ID=$(curl -s http://169.254.169.254/latest/meta-data/instance-id)
aws ec2 terminate-instances --region %s --instance-ids "$INSTANCE_ID"
`, opts.Region, ecrRegistry, imageRef, envStr, imageRef, opts.Region)

	return base64.StdEncoding.EncodeToString([]byte(script))
}
