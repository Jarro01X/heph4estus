package infra

import (
	"context"
	"fmt"
	"io"
	"strings"

	"heph4estus/internal/cloud"
)

// ImagePublisher handles registry authentication and image publication.
// Implementations exist for AWS ECR and selfhosted Docker registries.
type ImagePublisher interface {
	// Authenticate logs Docker into the target registry. For ECR this runs
	// the three-step AWS auth flow; for a selfhosted registry it runs
	// docker login with the controller credentials (or no-ops when no
	// credentials are configured).
	Authenticate(ctx context.Context) error

	// Publish tags the local image and pushes it to the remote registry.
	// Returns the remote image reference (e.g. "123.dkr.ecr.../nmap:latest"
	// or "10.0.1.5:5000/heph-nmap-worker:latest").
	Publish(ctx context.Context, localTag string, stream io.Writer) (remoteRef string, err error)
}

// ECRPublisher publishes images to AWS Elastic Container Registry.
type ECRPublisher struct {
	ECR     *ECRClient
	Docker  *DockerClient
	Region  string
	RepoURL string // ecr_repo_url from terraform outputs
}

func (p *ECRPublisher) Authenticate(ctx context.Context) error {
	return p.ECR.Authenticate(ctx, p.Region)
}

func (p *ECRPublisher) Publish(ctx context.Context, localTag string, stream io.Writer) (string, error) {
	if p.RepoURL == "" {
		return "", fmt.Errorf("ecr_repo_url is required for AWS image publish")
	}
	remoteTag := p.RepoURL + ":latest"
	if err := p.Docker.Tag(ctx, localTag, remoteTag); err != nil {
		return "", err
	}
	if err := p.Docker.Push(ctx, remoteTag, stream); err != nil {
		return "", err
	}
	return remoteTag, nil
}

// RegistryPublisher publishes images to a generic Docker registry
// (typically the controller-hosted registry for selfhosted deployments).
type RegistryPublisher struct {
	Docker      *DockerClient
	RegistryURL string // e.g. "10.0.1.5:5000" or "https://10.0.1.5:5000"
	Username    string // optional — empty means no auth
	Password    string // optional
}

func (p *RegistryPublisher) Authenticate(ctx context.Context) error {
	if p.Username == "" || p.Password == "" {
		// No credentials — assume insecure registry or pre-authenticated.
		return nil
	}
	return p.Docker.Login(ctx, dockerRegistryHost(p.RegistryURL), p.Username, p.Password)
}

func (p *RegistryPublisher) Publish(ctx context.Context, localTag string, stream io.Writer) (string, error) {
	if p.RegistryURL == "" {
		return "", fmt.Errorf("registry_url is required for selfhosted image publish")
	}
	remoteTag := dockerRegistryHost(p.RegistryURL) + "/" + localTag
	if err := p.Docker.Tag(ctx, localTag, remoteTag); err != nil {
		return "", err
	}
	if err := p.Docker.Push(ctx, remoteTag, stream); err != nil {
		return "", err
	}
	return remoteTag, nil
}

// NewPublisher constructs the appropriate ImagePublisher for the given cloud
// provider. For AWS it returns an ECRPublisher; for selfhosted/VPS providers
// it returns a RegistryPublisher that targets the controller-hosted Docker registry.
func NewPublisher(kind cloud.Kind, docker *DockerClient, ecr *ECRClient, outputs map[string]string, region string) ImagePublisher {
	if kind.IsSelfhostedFamily() {
		return &RegistryPublisher{
			Docker:      docker,
			RegistryURL: outputs["registry_url"],
			Username:    outputs["registry_username"],
			Password:    outputs["registry_password"],
		}
	}
	return &ECRPublisher{
		ECR:     ecr,
		Docker:  docker,
		Region:  region,
		RepoURL: outputs["ecr_repo_url"],
	}
}

func dockerRegistryHost(registryURL string) string {
	return strings.TrimPrefix(strings.TrimPrefix(registryURL, "https://"), "http://")
}
