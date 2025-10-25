package monitoring

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kljama/netscan/internal/state"
	"golang.org/x/time/rate"
)

type mockWriter struct {
	called           bool
	ip               string
	rtt              time.Duration
	success          bool
	deviceInfoCalled bool
	deviceIP         string
	deviceHostname   string
}

// Satisfy influx.Writer interface
func (m *mockWriter) WritePingResult(ip string, rtt time.Duration, successful bool) error {
	m.called = true
	m.ip = ip
	m.rtt = rtt
	m.success = successful
	return nil
}

func (m *mockWriter) WriteDeviceInfo(ip, hostname, sysDescr string) error {
	m.deviceInfoCalled = true
	m.deviceIP = ip
	m.deviceHostname = hostname
	return nil
}

type mockStateManager struct {
	lastSeenCalled bool
	lastSeenIP     string
}

func (m *mockStateManager) UpdateLastSeen(ip string) {
	m.lastSeenCalled = true
	m.lastSeenIP = ip
}

func TestStartPingerCancel(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("Test requires root privileges for ICMP ping")
	}
	dev := state.Device{IP: "127.0.0.1", Hostname: "localhost"}
	writer := &mockWriter{}
	stateMgr := &mockStateManager{}
	ctx, cancel := context.WithCancel(context.Background())
	limiter := rate.NewLimiter(rate.Limit(100.0), 256)
	var counter atomic.Int64
	go StartPinger(ctx, nil, dev, 10*time.Millisecond, 2*time.Second, writer, stateMgr, limiter, &counter)
	time.Sleep(30 * time.Millisecond)
	cancel()
	if !writer.called {
		t.Errorf("expected WritePingResult to be called")
	}
}
