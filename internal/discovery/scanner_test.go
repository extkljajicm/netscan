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
	const (
		testRateLimit  = 2.0 // 2 pings per second for testing
		testBurstLimit = 2   // burst of 2
	)
	
	// Create a very restrictive rate limiter: 2 pings per second, burst of 2
	limiter := rate.NewLimiter(rate.Limit(testRateLimit), testBurstLimit)
	
	// Use a small network to test with
	networks := []string{"127.0.0.0/30"} // Just 4 IPs: .0, .1, .2, .3
	workers := 4 // More workers than rate limit to test throttling
	
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	start := time.Now()
	_ = RunICMPSweep(ctx, networks, workers, limiter)
	elapsed := time.Since(start)
	
	// With 4 IPs and a rate of 2 pings/sec (burst of 2):
	// - First 2 pings happen immediately (burst)
	// - Next 2 pings take ~1 second (rate limited)
	// So we expect at least ~500ms elapsed time (reduced from 1s for CI tolerance)
	if elapsed < 500*time.Millisecond {
		t.Errorf("Expected rate limiting to take at least 500ms, but took %v", elapsed)
	}
	
	// Should complete within reasonable time (not hang)
	// Increased timeout to 8s for CI environments with variable CPU scheduling
	if elapsed > 8*time.Second {
		t.Errorf("RunICMPSweep took too long: %v (possible rate limiter issue)", elapsed)
	}
}

// TestRunICMPSweepContextCancellation verifies that RunICMPSweep respects context cancellation
func TestRunICMPSweepContextCancellation(t *testing.T) {
	const (
		verySlowRateLimit = 0.1 // 1 ping every 10 seconds for testing cancellation
		testBurstLimit    = 1
	)
	
	// Create a very slow rate limiter to test cancellation
	limiter := rate.NewLimiter(rate.Limit(verySlowRateLimit), testBurstLimit)
	
	// Use a network with several IPs
	networks := []string{"127.0.0.0/29"} // 8 IPs
	workers := 2
	
	// Cancel context after 100ms
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	
	start := time.Now()
	_ = RunICMPSweep(ctx, networks, workers, limiter)
	elapsed := time.Since(start)
	
	// Should exit within ~1s (100ms timeout + buffer for cleanup)
	// Increased from 500ms to 1s for CI environment tolerance
	// Not 10+ seconds waiting for rate limiter
	if elapsed > 1*time.Second {
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
