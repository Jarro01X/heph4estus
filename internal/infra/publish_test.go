package infra

import (
	"context"
	"testing"

	"heph4estus/internal/cloud"
)

func TestECRPublisher_Authenticate(t *testing.T) {
	// ECR auth requires 3 sequential calls: get-login-password, get-caller-identity, docker login.
	ecr := &ECRClient{
		runCmd: newSequentialMockExecutor([]mockCall{
			{stdout: "token"},
			{stdout: "123456789012"},
			{stdout: "Login Succeeded"},
		}),
		logger: nopLogger{},
	}
	pub := &ECRPublisher{
		ECR:     ecr,
		Docker:  &DockerClient{runCmd: newMockExecutor("", "", 0, nil), logger: nopLogger{}},
		Region:  "us-east-1",
		RepoURL: "123.dkr.ecr.us-east-1.amazonaws.com/nmap",
	}
	if err := pub.Authenticate(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestECRPublisher_Publish(t *testing.T) {
	cap, exec := newCapturingExecutor("")
	docker := &DockerClient{runCmd: exec, logger: nopLogger{}}
	pub := &ECRPublisher{
		Docker:  docker,
		RepoURL: "123.dkr.ecr.us-east-1.amazonaws.com/nmap",
	}

	ref, err := pub.Publish(context.Background(), "heph-nmap-worker:latest", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "123.dkr.ecr.us-east-1.amazonaws.com/nmap:latest"
	if ref != expected {
		t.Fatalf("expected ref %q, got %q", expected, ref)
	}
	// Should have called docker tag then docker push.
	if len(cap.calls) != 2 {
		t.Fatalf("expected 2 docker calls, got %d", len(cap.calls))
	}
	assertContains(t, cap.calls[0], "tag")
	assertContains(t, cap.calls[1], "push")
}

func TestECRPublisher_Publish_MissingRepoURL(t *testing.T) {
	pub := &ECRPublisher{
		Docker: &DockerClient{runCmd: newMockExecutor("", "", 0, nil), logger: nopLogger{}},
	}
	_, err := pub.Publish(context.Background(), "img:latest", nil)
	if err == nil {
		t.Fatal("expected error for missing ecr_repo_url")
	}
}

func TestRegistryPublisher_AuthenticateWithCredentials(t *testing.T) {
	cap, exec := newCapturingExecutor("")
	docker := &DockerClient{runCmd: exec, logger: nopLogger{}}
	pub := &RegistryPublisher{
		Docker:      docker,
		RegistryURL: "10.0.1.5:5000",
		Username:    "admin",
		Password:    "secret123",
	}

	if err := pub.Authenticate(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cap.calls) != 1 {
		t.Fatalf("expected 1 call (docker login), got %d", len(cap.calls))
	}
	assertContains(t, cap.calls[0], "docker")
	assertContains(t, cap.calls[0], "login")
	assertContains(t, cap.calls[0], "--username")
	assertContains(t, cap.calls[0], "admin")
	assertContains(t, cap.calls[0], "10.0.1.5:5000")
}

func TestRegistryPublisher_AuthenticateNoCredentials(t *testing.T) {
	// No credentials — should be a no-op (no docker login call).
	cap, exec := newCapturingExecutor("")
	docker := &DockerClient{runCmd: exec, logger: nopLogger{}}
	pub := &RegistryPublisher{
		Docker:      docker,
		RegistryURL: "10.0.1.5:5000",
	}

	if err := pub.Authenticate(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cap.calls) != 0 {
		t.Fatalf("expected 0 calls for no-credential auth, got %d", len(cap.calls))
	}
}

func TestRegistryPublisher_Publish(t *testing.T) {
	cap, exec := newCapturingExecutor("")
	docker := &DockerClient{runCmd: exec, logger: nopLogger{}}
	pub := &RegistryPublisher{
		Docker:      docker,
		RegistryURL: "10.0.1.5:5000",
	}

	ref, err := pub.Publish(context.Background(), "heph-nmap-worker:latest", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "10.0.1.5:5000/heph-nmap-worker:latest"
	if ref != expected {
		t.Fatalf("expected ref %q, got %q", expected, ref)
	}
	if len(cap.calls) != 2 {
		t.Fatalf("expected 2 docker calls, got %d", len(cap.calls))
	}
	assertContains(t, cap.calls[0], "tag")
	assertContains(t, cap.calls[1], "push")
}

func TestRegistryPublisher_PublishStripsSchemeForDocker(t *testing.T) {
	cap, exec := newCapturingExecutor("")
	docker := &DockerClient{runCmd: exec, logger: nopLogger{}}
	pub := &RegistryPublisher{
		Docker:      docker,
		RegistryURL: "https://10.0.1.5:5000",
	}

	ref, err := pub.Publish(context.Background(), "heph-nmap-worker:latest", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "10.0.1.5:5000/heph-nmap-worker:latest"
	if ref != expected {
		t.Fatalf("expected ref %q, got %q", expected, ref)
	}
	assertContains(t, cap.calls[0], expected)
}

func TestRegistryPublisher_Publish_MissingRegistryURL(t *testing.T) {
	pub := &RegistryPublisher{
		Docker: &DockerClient{runCmd: newMockExecutor("", "", 0, nil), logger: nopLogger{}},
	}
	_, err := pub.Publish(context.Background(), "img:latest", nil)
	if err == nil {
		t.Fatal("expected error for missing registry_url")
	}
}

func TestNewPublisher_AWS(t *testing.T) {
	outputs := map[string]string{"ecr_repo_url": "123.dkr.ecr.us-east-1.amazonaws.com/nmap"}
	docker := &DockerClient{runCmd: newMockExecutor("", "", 0, nil), logger: nopLogger{}}
	ecr := &ECRClient{runCmd: newMockExecutor("", "", 0, nil), logger: nopLogger{}}

	pub := NewPublisher(cloud.KindAWS, docker, ecr, outputs, "us-east-1")
	if _, ok := pub.(*ECRPublisher); !ok {
		t.Fatalf("expected *ECRPublisher, got %T", pub)
	}
}

func TestNewPublisher_Selfhosted(t *testing.T) {
	outputs := map[string]string{
		"registry_url":      "10.0.1.5:5000",
		"registry_username": "admin",
		"registry_password": "secret",
	}
	docker := &DockerClient{runCmd: newMockExecutor("", "", 0, nil), logger: nopLogger{}}
	ecr := &ECRClient{runCmd: newMockExecutor("", "", 0, nil), logger: nopLogger{}}

	pub := NewPublisher(cloud.KindSelfhosted, docker, ecr, outputs, "")
	rp, ok := pub.(*RegistryPublisher)
	if !ok {
		t.Fatalf("expected *RegistryPublisher, got %T", pub)
	}
	if rp.RegistryURL != "10.0.1.5:5000" {
		t.Fatalf("expected registry URL 10.0.1.5:5000, got %q", rp.RegistryURL)
	}
	if rp.Username != "admin" {
		t.Fatalf("expected username admin, got %q", rp.Username)
	}
}

func TestNewPublisher_EmptyKindDefaultsToAWS(t *testing.T) {
	outputs := map[string]string{"ecr_repo_url": "123.dkr.ecr.us-east-1.amazonaws.com/nmap"}
	docker := &DockerClient{runCmd: newMockExecutor("", "", 0, nil), logger: nopLogger{}}
	ecr := &ECRClient{runCmd: newMockExecutor("", "", 0, nil), logger: nopLogger{}}

	pub := NewPublisher("", docker, ecr, outputs, "us-east-1")
	if _, ok := pub.(*ECRPublisher); !ok {
		t.Fatalf("empty kind should default to ECRPublisher, got %T", pub)
	}
}

func TestDockerLogin(t *testing.T) {
	cap, exec := newCapturingExecutor("")
	dc := &DockerClient{runCmd: exec, logger: nopLogger{}}

	if err := dc.Login(context.Background(), "registry.example.com", "user", "pass"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cap.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(cap.calls))
	}
	assertContains(t, cap.calls[0], "docker")
	assertContains(t, cap.calls[0], "login")
	assertContains(t, cap.calls[0], "--username")
	assertContains(t, cap.calls[0], "user")
	assertContains(t, cap.calls[0], "--password")
	assertContains(t, cap.calls[0], "pass")
	assertContains(t, cap.calls[0], "registry.example.com")
}
