package core

import (
	"sync"
	"testing"
)

func TestStreamWriter_WriteDrain(t *testing.T) {
	sw := &StreamWriter{}

	n, err := sw.Write([]byte("hello "))
	if err != nil || n != 6 {
		t.Fatalf("Write returned (%d, %v)", n, err)
	}
	sw.Write([]byte("world")) //nolint:errcheck

	got := sw.Drain()
	if got != "hello world" {
		t.Fatalf("expected 'hello world', got %q", got)
	}

	// After drain, buffer should be empty.
	if s := sw.Drain(); s != "" {
		t.Fatalf("expected empty after drain, got %q", s)
	}
}

func TestStreamWriter_ConcurrentWrites(t *testing.T) {
	sw := &StreamWriter{}
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sw.Write([]byte("x")) //nolint:errcheck
		}()
	}
	wg.Wait()

	got := sw.Drain()
	if len(got) != 100 {
		t.Fatalf("expected 100 bytes, got %d", len(got))
	}
}

func TestStreamWriter_DrainWhileWriting(t *testing.T) {
	sw := &StreamWriter{}
	var wg sync.WaitGroup

	// Writer goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			sw.Write([]byte("a")) //nolint:errcheck
		}
	}()

	// Drain goroutine — just ensure no panics
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			sw.Drain()
		}
	}()

	wg.Wait()
}
