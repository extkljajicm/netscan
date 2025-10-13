package influx

import (
	"testing"
	"time"
)

type mockWriter struct {
	lastIP       string
	lastHostname string
	lastRTT      time.Duration
	lastSuccess  bool
	called       bool
}

func (m *mockWriter) WritePingResult(ip, hostname string, rtt time.Duration, successful bool) error {
	m.lastIP = ip
	m.lastHostname = hostname
	m.lastRTT = rtt
	m.lastSuccess = successful
	m.called = true
	return nil
}

func TestWritePingResult(t *testing.T) {
	mw := &mockWriter{}
	ip := "1.2.3.4"
	host := "host"
	rtt := 42 * time.Millisecond
	success := true
	mw.WritePingResult(ip, host, rtt, success)
	if !mw.called || mw.lastIP != ip || mw.lastHostname != host || mw.lastRTT != rtt || mw.lastSuccess != success {
		t.Errorf("mockWriter did not record values correctly")
	}
}
