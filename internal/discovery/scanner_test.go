package discovery

import (
	"context"
	"net"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestIpsFromCIDR(t *testing.T) {
	ips := ipsFromCIDR("192.168.1.0/30")
	want := []string{"192.168.1.0", "192.168.1.1", "192.168.1.2", "192.168.1.3"}
	if len(ips) != len(want) {
		t.Fatalf("expected %d IPs, got %d", len(want), len(ips))
	}
	for i, ip := range want {
		if ips[i] != ip {
			t.Errorf("expected %s, got %s", ip, ips[i])
		}
	}
}

func TestIncIP(t *testing.T) {
	ip := net.ParseIP("192.168.1.1")
	incIP(ip)
	if ip.String() != "192.168.1.2" {
		t.Errorf("expected 192.168.1.2, got %s", ip.String())
	}
}

// TestRunICMPSweepWithRateLimiter verifies that RunICMPSweep respects the rate limiter
func TestRunICMPSweepWithRateLimiter(t *testing.T) {
	// Create a very restrictive rate limiter: 2 pings per second, burst of 2
	limiter := rate.NewLimiter(rate.Limit(2.0), 2)
	
	// Use a small network to test with
	networks := []string{"127.0.0.0/30"} // Just 4 IPs: .0, .1, .2, .3
	workers := 4 // More workers than rate limit to test throttling
	
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	start := time.Now()
	_ = RunICMPSweep(ctx, networks, workers, limiter)
	elapsed := time.Since(start)
	
	// With 4 IPs and a rate of 2 pings/sec (burst of 2):
	// - First 2 pings happen immediately (burst)
	// - Next 2 pings take ~1 second (rate limited)
	// So we expect at least ~1 second elapsed time
	if elapsed < 500*time.Millisecond {
		t.Errorf("Expected rate limiting to take at least 500ms, but took %v", elapsed)
	}
	
	// Should complete within reasonable time (not hang)
	if elapsed > 4*time.Second {
		t.Errorf("RunICMPSweep took too long: %v (possible rate limiter issue)", elapsed)
	}
}

// TestRunICMPSweepContextCancellation verifies that RunICMPSweep respects context cancellation
func TestRunICMPSweepContextCancellation(t *testing.T) {
	// Create a very slow rate limiter to test cancellation
	limiter := rate.NewLimiter(rate.Limit(0.1), 1) // 1 ping every 10 seconds
	
	// Use a network with several IPs
	networks := []string{"127.0.0.0/29"} // 8 IPs
	workers := 2
	
	// Cancel context after 100ms
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	
	start := time.Now()
	_ = RunICMPSweep(ctx, networks, workers, limiter)
	elapsed := time.Since(start)
	
	// Should exit within ~200ms (100ms timeout + some buffer for cleanup)
	// Not 10+ seconds waiting for rate limiter
	if elapsed > 500*time.Millisecond {
		t.Errorf("RunICMPSweep did not respect context cancellation, took %v", elapsed)
	}
}

// TestRunICMPSweepWithoutRateLimiter verifies that RunICMPSweep works with nil limiter
func TestRunICMPSweepWithoutRateLimiter(t *testing.T) {
	networks := []string{"127.0.0.0/30"} // Just 4 IPs
	workers := 2
	
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	
	// Should work fine with nil limiter (no rate limiting)
	_ = RunICMPSweep(ctx, networks, workers, nil)
	// No assertions needed - just verify it doesn't panic
}
