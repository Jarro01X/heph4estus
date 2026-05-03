package infra

import (
	"context"
	"fmt"
	"io"
	"slices"

	"heph4estus/internal/logger"
)

// DockerClient wraps the docker CLI binary.
type DockerClient struct {
	runCmd CommandExecutor
	logger logger.Logger
}

// NewDockerClient creates a DockerClient using DefaultExecutor.
func NewDockerClient(logger logger.Logger) *DockerClient {
	return &DockerClient{
		runCmd: DefaultExecutor,
		logger: logger,
	}
}

// Build builds a Docker image from the given Dockerfile and build context.
func (d *DockerClient) Build(ctx context.Context, dockerfile, buildContext, tag string, stream io.Writer) error {
	d.logger.Info("Building Docker image %s", tag)
	result, err := d.runCmd(ctx, "", stream, "docker", "build", "-f", dockerfile, "-t", tag, buildContext)
	if err != nil {
		d.logger.Error("docker build failed: %s", string(result.Stderr))
		return fmt.Errorf("docker build: %w", err)
	}
	return nil
}

// BuildWithArgs builds a Docker image with additional --build-arg flags.
func (d *DockerClient) BuildWithArgs(ctx context.Context, dockerfile, buildContext, tag string, buildArgs map[string]string, stream io.Writer) error {
	d.logger.Info("Building Docker image %s", tag)
	args := []string{"docker", "build", "-f", dockerfile, "-t", tag}
	keys := make([]string, 0, len(buildArgs))
	for k := range buildArgs {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		args = append(args, "--build-arg", k+"="+buildArgs[k])
	}
	args = append(args, buildContext)
	result, err := d.runCmd(ctx, "", stream, args...)
	if err != nil {
		d.logger.Error("docker build failed: %s", string(result.Stderr))
		return fmt.Errorf("docker build: %w", err)
	}
	return nil
}

// Tag tags a Docker image.
func (d *DockerClient) Tag(ctx context.Context, source, target string) error {
	d.logger.Info("Tagging Docker image %s -> %s", source, target)
	result, err := d.runCmd(ctx, "", nil, "docker", "tag", source, target)
	if err != nil {
		d.logger.Error("docker tag failed: %s", string(result.Stderr))
		return fmt.Errorf("docker tag: %w", err)
	}
	return nil
}

// Login authenticates Docker to a registry with the given credentials.
func (d *DockerClient) Login(ctx context.Context, registry, username, password string) error {
	d.logger.Info("Logging into registry %s", registry)
	result, err := d.runCmd(ctx, "", nil, "docker", "login", "--username", username, "--password", password, registry)
	if err != nil {
		d.logger.Error("docker login failed: %s", string(result.Stderr))
		return fmt.Errorf("docker login: %w", err)
	}
	return nil
}

// Push pushes a Docker image to a registry.
func (d *DockerClient) Push(ctx context.Context, tag string, stream io.Writer) error {
	d.logger.Info("Pushing Docker image %s", tag)
	result, err := d.runCmd(ctx, "", stream, "docker", "push", tag)
	if err != nil {
		d.logger.Error("docker push failed: %s", string(result.Stderr))
		return fmt.Errorf("docker push: %w", err)
	}
	return nil
}
