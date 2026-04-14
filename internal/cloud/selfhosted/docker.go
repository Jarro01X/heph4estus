package selfhosted

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"heph4estus/internal/cloud"
	"heph4estus/internal/logger"
)

var (
	errComputeNotConfigured = errors.New("selfhosted: compute is not configured")
	errSpotUnsupported      = fmt.Errorf("selfhosted: spot instances are not supported: %w", cloud.ErrNotImplemented)
)

var (
	_ cloud.Compute = (*DockerCompute)(nil)
	_ cloud.Compute = configErrorCompute{}
)

// CommandRunner executes a command on a remote host. The real implementation
// shells out to the ssh binary; tests inject a recorder.
type CommandRunner interface {
	Run(ctx context.Context, host string, remoteCmd string) error
}

type sshRunner struct {
	user    string
	keyPath string
	port    int
}

func (r *sshRunner) Run(ctx context.Context, host string, remoteCmd string) error {
	port := r.port
	if port == 0 {
		port = 22
	}
	args := []string{
		"-i", r.keyPath,
		"-p", strconv.Itoa(port),
		"-o", "StrictHostKeyChecking=no",
		"-o", "BatchMode=yes",
		fmt.Sprintf("%s@%s", r.user, host),
		remoteCmd,
	}
	cmd := exec.CommandContext(ctx, "ssh", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ssh %s: %w: %s", host, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// DockerCompute implements cloud.Compute by running Docker containers on
// remote hosts over SSH.
type DockerCompute struct {
	hosts        []string
	image        string
	transportEnv map[string]string
	runner       CommandRunner
	logger       logger.Logger
}

func newDockerCompute(cfg ComputeConfig, transportEnv map[string]string, runner CommandRunner, log logger.Logger) *DockerCompute {
	return &DockerCompute{
		hosts:        cfg.WorkerHosts,
		image:        cfg.DockerImage,
		transportEnv: transportEnv,
		runner:       runner,
		logger:       log,
	}
}

// RunContainer launches detached Docker containers on remote worker hosts
// over SSH, round-robining across the configured host list.
func (c *DockerCompute) RunContainer(ctx context.Context, opts cloud.ContainerOpts) (string, error) {
	count := opts.Count
	if count <= 0 {
		count = 1
	}

	image := c.image
	if opts.Image != "" {
		image = opts.Image
	}

	env := c.buildEnv(opts.Env)

	var launched []string
	for i := 0; i < count; i++ {
		host := c.hosts[i%len(c.hosts)]
		name := containerName(opts.ContainerName, i, count)

		c.logger.Info("Pulling %s on %s", image, host)
		if err := c.runner.Run(ctx, host, "docker pull "+image); err != nil {
			return strings.Join(launched, ","), fmt.Errorf("docker pull on %s: %w", host, err)
		}

		c.logger.Info("Starting container %s on %s", name, host)
		if err := c.runner.Run(ctx, host, buildDockerRun(name, env, image)); err != nil {
			return strings.Join(launched, ","), fmt.Errorf("docker run on %s: %w", host, err)
		}

		launched = append(launched, host+":"+name)
	}

	return strings.Join(launched, ","), nil
}

func (c *DockerCompute) RunSpotInstances(_ context.Context, _ cloud.SpotOpts) ([]string, error) {
	return nil, errSpotUnsupported
}

func (c *DockerCompute) GetSpotStatus(_ context.Context, _ []string) ([]cloud.SpotStatus, error) {
	return nil, errSpotUnsupported
}

// buildEnv merges caller env, transport env, and forces CLOUD=selfhosted.
// Precedence: opts.Env < transportEnv < CLOUD=selfhosted.
func (c *DockerCompute) buildEnv(optsEnv map[string]string) map[string]string {
	env := make(map[string]string, len(optsEnv)+len(c.transportEnv)+1)
	for k, v := range optsEnv {
		env[k] = v
	}
	for k, v := range c.transportEnv {
		if v != "" {
			env[k] = v
		}
	}
	env["CLOUD"] = "selfhosted"
	return env
}

func buildDockerRun(name string, env map[string]string, image string) string {
	parts := []string{"docker", "run", "-d", "--name", name}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		parts = append(parts, "-e", k+"="+env[k])
	}
	parts = append(parts, image)
	return strings.Join(parts, " ")
}

var nameRe = regexp.MustCompile(`[^a-zA-Z0-9_.-]`)

func containerName(base string, index, total int) string {
	if base == "" {
		base = "heph-worker"
	}
	name := nameRe.ReplaceAllString(base, "-")
	if total > 1 {
		name = fmt.Sprintf("%s-%d", name, index)
	}
	return name
}

func validateComputeConfig(cfg *ComputeConfig) error {
	if cfg == nil {
		return errComputeNotConfigured
	}
	if len(cfg.WorkerHosts) == 0 {
		return fmt.Errorf("%w: no worker hosts configured", errComputeNotConfigured)
	}
	if cfg.SSHUser == "" {
		return fmt.Errorf("%w: no SSH user configured", errComputeNotConfigured)
	}
	if cfg.SSHKeyPath == "" {
		return fmt.Errorf("%w: no SSH key path configured", errComputeNotConfigured)
	}
	if cfg.DockerImage == "" {
		return fmt.Errorf("%w: no Docker image configured", errComputeNotConfigured)
	}
	return nil
}

// configErrorCompute implements cloud.Compute by returning a configuration
// error for all operations. Used when compute config is absent or incomplete.
type configErrorCompute struct {
	err error
}

func (c configErrorCompute) RunContainer(_ context.Context, _ cloud.ContainerOpts) (string, error) {
	return "", c.err
}

func (c configErrorCompute) RunSpotInstances(_ context.Context, _ cloud.SpotOpts) ([]string, error) {
	return nil, c.err
}

func (c configErrorCompute) GetSpotStatus(_ context.Context, _ []string) ([]cloud.SpotStatus, error) {
	return nil, c.err
}
