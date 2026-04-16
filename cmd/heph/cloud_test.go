package main

import (
	"testing"

	"heph4estus/internal/cloud"
)

func TestValidateComputeMode_AWS(t *testing.T) {
	for _, mode := range []string{"", "auto", "fargate", "spot"} {
		if err := ValidateComputeMode(cloud.KindAWS, mode); err != nil {
			t.Errorf("AWS mode %q should be valid: %v", mode, err)
		}
	}
}

func TestValidateComputeMode_AWSInvalid(t *testing.T) {
	if err := ValidateComputeMode(cloud.KindAWS, "gpu"); err == nil {
		t.Fatal("expected error for invalid AWS compute mode")
	}
}

func TestValidateComputeMode_ManualAuto(t *testing.T) {
	for _, mode := range []string{"", "auto"} {
		if err := ValidateComputeMode(cloud.KindManual, mode); err != nil {
			t.Errorf("manual mode %q should be valid: %v", mode, err)
		}
	}
}

func TestValidateComputeMode_ManualFargateRejected(t *testing.T) {
	err := ValidateComputeMode(cloud.KindManual, "fargate")
	if err == nil {
		t.Fatal("expected error for manual + fargate")
	}
}

func TestValidateComputeMode_ProviderSpotRejected(t *testing.T) {
	err := ValidateComputeMode(cloud.KindHetzner, "spot")
	if err == nil {
		t.Fatal("expected error for hetzner + spot")
	}
}

func TestRequireDeploySupport_HetznerAllowed(t *testing.T) {
	if err := requireDeploySupport(cloud.KindHetzner); err != nil {
		t.Fatalf("Hetzner deploy should be allowed (provider-native): %v", err)
	}
}

func TestRequireDeploySupport_ManualBlocked(t *testing.T) {
	err := requireDeploySupport(cloud.KindManual)
	if err == nil {
		t.Fatal("expected error: manual deploy should be blocked")
	}
}

func TestRequireDeploySupport_AWSAllowed(t *testing.T) {
	if err := requireDeploySupport(cloud.KindAWS); err != nil {
		t.Fatalf("AWS deploy should be allowed: %v", err)
	}
}
