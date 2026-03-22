package nmap

import (
	"testing"
	"time"
)

func TestJitterDuration_Disabled(t *testing.T) {
	if d := JitterDuration(0); d != 0 {
		t.Fatalf("expected 0 when disabled, got %v", d)
	}
}

func TestJitterDuration_Negative(t *testing.T) {
	if d := JitterDuration(-5); d != 0 {
		t.Fatalf("expected 0 for negative, got %v", d)
	}
}

func TestJitterDuration_BoundedRange(t *testing.T) {
	maxSeconds := 2
	for i := 0; i < 100; i++ {
		d := JitterDuration(maxSeconds)
		if d < 0 || d >= time.Duration(maxSeconds)*time.Second {
			t.Fatalf("iteration %d: jitter %v out of range [0, %ds)", i, d, maxSeconds)
		}
	}
}

func TestJitterDuration_NonZeroForLargeMax(t *testing.T) {
	// With maxSeconds=60, it's astronomically unlikely all 100 iterations return 0.
	nonZero := 0
	for i := 0; i < 100; i++ {
		if JitterDuration(60) > 0 {
			nonZero++
		}
	}
	if nonZero == 0 {
		t.Fatal("expected at least one non-zero jitter in 100 iterations")
	}
}
