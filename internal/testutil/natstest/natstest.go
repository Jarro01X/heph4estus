package natstest

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

const startupTimeout = 10 * time.Second

type Options struct {
	JetStream bool
}

func Start(t *testing.T, opts Options) *natsserver.Server {
	t.Helper()

	serverOpts := &natsserver.Options{
		Host:       "127.0.0.1",
		Port:       -1,
		DontListen: true,
		NoSigs:     true,
		NoLog:      true,
	}
	if opts.JetStream {
		serverOpts.JetStream = true
		serverOpts.StoreDir = t.TempDir()
	}

	srv, err := natsserver.NewServer(serverOpts)
	if err != nil {
		t.Fatalf("embedded nats: %v", err)
	}

	log := &memoryLogger{}
	srv.SetLogger(log, false, false)

	go srv.Start()
	if !srv.ReadyForConnections(startupTimeout) {
		srv.Shutdown()
		srv.WaitForShutdown()
		t.Fatalf("embedded nats not ready after %s (jetstream=%t, url=%q): %s", startupTimeout, opts.JetStream, srv.ClientURL(), log.String())
	}

	probe := Connect(t, srv, nats.Name("heph-test-readiness-probe"))
	probe.Close()

	t.Cleanup(func() {
		srv.Shutdown()
		srv.WaitForShutdown()
	})
	return srv
}

func Connect(t *testing.T, srv *natsserver.Server, opts ...nats.Option) *nats.Conn {
	t.Helper()
	connectOpts := []nats.Option{
		nats.SetCustomDialer(inProcessDialer{srv: srv}),
		nats.NoReconnect(),
		nats.Timeout(2 * time.Second),
	}
	connectOpts = append(connectOpts, opts...)
	nc, err := nats.Connect("nats://in-process", connectOpts...)
	if err != nil {
		t.Fatalf("embedded nats connect: %v", err)
	}
	return nc
}

type inProcessDialer struct {
	srv *natsserver.Server
}

func (d inProcessDialer) Dial(_, _ string) (net.Conn, error) {
	if d.srv == nil {
		return nil, fmt.Errorf("embedded nats server is nil")
	}
	return d.srv.InProcessConn()
}

type memoryLogger struct {
	mu    sync.Mutex
	lines []string
}

func (l *memoryLogger) Noticef(format string, v ...any) { l.append("notice", format, v...) }
func (l *memoryLogger) Warnf(format string, v ...any)   { l.append("warn", format, v...) }
func (l *memoryLogger) Fatalf(format string, v ...any)  { l.append("fatal", format, v...) }
func (l *memoryLogger) Errorf(format string, v ...any)  { l.append("error", format, v...) }
func (l *memoryLogger) Debugf(format string, v ...any)  { l.append("debug", format, v...) }
func (l *memoryLogger) Tracef(format string, v ...any)  { l.append("trace", format, v...) }

func (l *memoryLogger) append(level, format string, v ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, level+": "+fmt.Sprintf(format, v...))
}

func (l *memoryLogger) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.lines) == 0 {
		return "no server logs captured"
	}
	return strings.Join(l.lines, "; ")
}
