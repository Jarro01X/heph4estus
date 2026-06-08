package main

import (
	"strings"
	"testing"
)

func TestAWSImageTagFromOutputs(t *testing.T) {
	tag, err := awsImageTagFromOutputs(map[string]string{
		"image_tag": " heph-nmap-worker-20260608T032422Z-a1b2c3d4 ",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tag != "heph-nmap-worker-20260608T032422Z-a1b2c3d4" {
		t.Fatalf("tag = %q", tag)
	}
}

func TestAWSImageTagFromOutputsMissing(t *testing.T) {
	_, err := awsImageTagFromOutputs(map[string]string{})
	if err == nil {
		t.Fatal("expected missing image_tag error")
	}
	if !strings.Contains(err.Error(), "image_tag") {
		t.Fatalf("error = %v, want image_tag", err)
	}
}
