package state

import (
	"testing"
	"time"
)

// TestDoubleDecrementBug - Tests the bug where calling GetSuspendedCount() to clean up
// expired suspensions, followed by ReportPingSuccess(), causes the counter to decrement twice
func TestDoubleDecrementBug(t *testing.T) {
	mgr := NewManager(1000)

	// Add two devices
	dev1 := Device{
		IP:       "192.168.1.1",
		Hostname: "device1",
		LastSeen: time.Now(),
	}
	dev2 := Device{
		IP:       "192.168.1.2",
		Hostname: "device2",
		LastSeen: time.Now(),
	}
	mgr.Add(dev1)
	mgr.Add(dev2)

	// Trip the circuit breaker for both devices
	maxFails := 3
	backoff := 100 * time.Millisecond
	for i := 0; i < maxFails; i++ {
		mgr.ReportPingFail("192.168.1.1", maxFails, backoff)
		mgr.ReportPingFail("192.168.1.2", maxFails, backoff)
	}

	// Both devices should be suspended
	if !mgr.IsSuspended("192.168.1.1") || !mgr.IsSuspended("192.168.1.2") {
		t.Fatal("Both devices should be suspended")
	}
	
	initialCount := mgr.GetSuspendedCount()
	t.Logf("Initial suspended count: %d", initialCount)
	if initialCount != 2 {
		t.Errorf("Expected 2 suspended devices, got %d", initialCount)
	}

	// Wait for suspensions to expire
	time.Sleep(backoff + 50*time.Millisecond)

	// Call GetSuspendedCount() which triggers cleanupExpiredSuspensions()
	// This should clean up both expired suspensions and decrement counter to 0
	countAfterCleanup := mgr.GetSuspendedCount()
	t.Logf("Count after cleanup via GetSuspendedCount(): %d", countAfterCleanup)
	if countAfterCleanup != 0 {
		t.Errorf("Expected 0 after cleanup, got %d", countAfterCleanup)
	}

	// Now report successful ping for device1
	// BUG: This will decrement the counter again, even though it was already decremented
	mgr.ReportPingSuccess("192.168.1.1")

	// Check the count - it should still be 0, not -1
	countAfterSuccess := mgr.GetSuspendedCount()
	t.Logf("Count after ReportPingSuccess(): %d", countAfterSuccess)
	
	if countAfterSuccess < 0 {
		t.Errorf("BUG DETECTED: Counter went negative! Got %d", countAfterSuccess)
	}
	
	if countAfterSuccess != 0 {
		t.Errorf("Expected count to remain 0, got %d", countAfterSuccess)
	}

	// Verify with accurate count
	accurateCount := mgr.GetSuspendedCountAccurate()
	t.Logf("Accurate count: %d", accurateCount)
	if accurateCount != 0 {
		t.Errorf("Accurate count should be 0, got %d", accurateCount)
	}
}

// TestDoubleDecrementMultipleDevices - Tests with multiple devices to show counter corruption
func TestDoubleDecrementMultipleDevices(t *testing.T) {
	mgr := NewManager(1000)

	// Add 5 devices
	for i := 1; i <= 5; i++ {
		dev := Device{
			IP:       "192.168.1." + string(rune(i)),
			Hostname: "device",
			LastSeen: time.Now(),
		}
		mgr.Add(dev)
	}

	// Trip circuit breaker for all 5 devices
	maxFails := 3
	backoff := 100 * time.Millisecond
	for i := 1; i <= 5; i++ {
		ip := "192.168.1." + string(rune(i))
		for j := 0; j < maxFails; j++ {
			mgr.ReportPingFail(ip, maxFails, backoff)
		}
	}

	// All 5 should be suspended
	count := mgr.GetSuspendedCount()
	t.Logf("Initial count: %d", count)
	if count != 5 {
		t.Errorf("Expected 5 suspended devices, got %d", count)
	}

	// Wait for suspensions to expire
	time.Sleep(backoff + 50*time.Millisecond)

	// Call GetSuspendedCount() to trigger cleanup
	countAfterExpiry := mgr.GetSuspendedCount()
	t.Logf("Count after expiry cleanup: %d", countAfterExpiry)

	// Now report successful pings for all 5 devices
	for i := 1; i <= 5; i++ {
		ip := "192.168.1." + string(rune(i))
		mgr.ReportPingSuccess(ip)
	}

	// Check final count
	finalCount := mgr.GetSuspendedCount()
	t.Logf("Final count: %d", finalCount)

	// Counter might be negative due to double decrement
	if finalCount < 0 {
		t.Errorf("BUG: Counter went negative! Got %d", finalCount)
	}

	if finalCount != 0 {
		t.Errorf("Expected final count 0, got %d", finalCount)
	}
}
