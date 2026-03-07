package core

import "sync"

// StreamWriter is a concurrency-safe io.Writer that buffers output for
// periodic draining into a TUI viewport via TickMsg.
type StreamWriter struct {
	mu  sync.Mutex
	buf []byte
}

// Write appends p to the internal buffer. Safe for concurrent use.
func (sw *StreamWriter) Write(p []byte) (int, error) {
	sw.mu.Lock()
	sw.buf = append(sw.buf, p...)
	sw.mu.Unlock()
	return len(p), nil
}

// Drain returns accumulated content and resets the buffer.
func (sw *StreamWriter) Drain() string {
	sw.mu.Lock()
	s := string(sw.buf)
	sw.buf = sw.buf[:0]
	sw.mu.Unlock()
	return s
}
