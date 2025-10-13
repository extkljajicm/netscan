package monitoring

import (
	"context"
	"testing"
	"time"

	"github.com/marko/netscan/internal/config"
	"github.com/marko/netscan/internal/state"
)

type mockWriter struct {
	called   bool
	ip       string
	hostname string
	rtt      time.Duration
	success  bool
}

// Satisfy influx.Writer interface
func (m *mockWriter) WritePingResult(ip, hostname string, rtt time.Duration, successful bool) error {
	m.called = true
	m.ip = ip
	m.hostname = hostname
	m.rtt = rtt
	m.success = successful
	return nil
}

func TestStartPingerCancel(t *testing.T) {
	dev := state.Device{IP: "127.0.0.1", Hostname: "localhost"}
	cfg := &config.Config{PingInterval: 10 * time.Millisecond, PingTimeout: 1 * time.Millisecond}
	writer := &mockWriter{}
	ctx, cancel := context.WithCancel(context.Background())
	go StartPinger(dev, cfg, writer, ctx)
	time.Sleep(30 * time.Millisecond)
	cancel()
	if !writer.called {
		t.Errorf("expected WritePingResult to be called")
	}
}
