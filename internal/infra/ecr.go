package infra

import (
	"context"
	"fmt"
	"strings"

	"heph4estus/internal/logger"
)

// ECRClient wraps the aws CLI for ECR authentication and repository lookups.
type ECRClient struct {
	runCmd CommandExecutor
	logger logger.Logger
}

// NewECRClient creates an ECRClient using DefaultExecutor.
func NewECRClient(logger logger.Logger) *ECRClient {
	return &ECRClient{
		runCmd: DefaultExecutor,
		logger: logger,
	}
}

// Authenticate logs Docker into ECR for the given region. It runs three sequential
// commands: get-login-password, get-caller-identity (for account ID), and docker login.
func (e *ECRClient) Authenticate(ctx context.Context, region string) error {
	e.logger.Info("Authenticating Docker to ECR in %s", region)

	// Step 1: Get ECR login password.
	tokenResult, err := e.runCmd(ctx, "", nil, "aws", "ecr", "get-login-password", "--region", region)
	if err != nil {
		e.logger.Error("ecr get-login-password failed: %s", string(tokenResult.Stderr))
		return fmt.Errorf("ecr get-login-password: %w", err)
	}
	token := strings.TrimSpace(string(tokenResult.Stdout))

	// Step 2: Get account ID via STS.
	identityResult, err := e.runCmd(ctx, "", nil, "aws", "sts", "get-caller-identity", "--query", "Account", "--output", "text")
	if err != nil {
		e.logger.Error("sts get-caller-identity failed: %s", string(identityResult.Stderr))
		return fmt.Errorf("sts get-caller-identity: %w", err)
	}
	accountID := strings.TrimSpace(string(identityResult.Stdout))

	// Step 3: Docker login to ECR registry.
	registry := fmt.Sprintf("%s.dkr.ecr.%s.amazonaws.com", accountID, region)
	loginResult, err := e.runCmd(ctx, "", nil, "docker", "login", "--username", "AWS", "--password", token, registry)
	if err != nil {
		e.logger.Error("docker login failed: %s", string(loginResult.Stderr))
		return fmt.Errorf("docker login: %w", err)
	}

	e.logger.Info("Authenticated to ECR registry %s", registry)
	return nil
}

// GetRepoURI returns the repository URI for the given ECR repository name and region.
func (e *ECRClient) GetRepoURI(ctx context.Context, region, repoName string) (string, error) {
	e.logger.Info("Looking up ECR repository URI for %s in %s", repoName, region)
	result, err := e.runCmd(ctx, "", nil,
		"aws", "ecr", "describe-repositories",
		"--repository-names", repoName,
		"--region", region,
		"--query", "repositories[0].repositoryUri",
		"--output", "text",
	)
	if err != nil {
		e.logger.Error("ecr describe-repositories failed: %s", string(result.Stderr))
		return "", fmt.Errorf("ecr describe-repositories: %w", err)
	}
	return strings.TrimSpace(string(result.Stdout)), nil
}
