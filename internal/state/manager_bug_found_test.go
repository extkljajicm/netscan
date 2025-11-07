package state

import (
	"testing"
	"time"
)

// TestBugFoundAdd_ExpiredSuspensionNotDecremented - Tests the bug where Add() with an 
// existing device that has an EXPIRED suspension does not decrement the counter
func TestBugFoundAdd_ExpiredSuspensionNotDecremented(t *testing.T) {
	mgr := NewManager(1000)

	// Add a device first
	dev2 := Device{
		IP:             "192.168.1.100",
		Hostname:       "test-device",
		LastSeen:       time.Now(),
	}
	mgr.Add(dev2)
	
	// Suspend the device
	maxFails := 3
	backoff := 100 * time.Millisecond
	for i := 0; i < maxFails; i++ {
		mgr.ReportPingFail("192.168.1.100", maxFails, backoff)
	}
	
	// Counter should be 1
	count1 := mgr.GetSuspendedCount()
	t.Logf("Count after suspension: %d", count1)
	if count1 != 1 {
		t.Fatalf("Expected count 1, got %d", count1)
	}
	
	// Wait for suspension to expire
	time.Sleep(backoff + 50*time.Millisecond)
	
	// Now the suspension is expired, but cleanup hasn't run
	// Verify IsSuspended returns false
	if mgr.IsSuspended("192.168.1.100") {
		t.Error("Device should not be suspended after expiration")
	}
	
	// But the counter is still 1 (cleanup hasn't run)
	// DON'T call GetSuspendedCount() here because that would trigger cleanup
	
	// Verify device state - SuspendedUntil should still be set (not cleaned up yet)
	retrieved, _ := mgr.Get("192.168.1.100")
	if retrieved.SuspendedUntil.IsZero() {
		t.Fatal("SuspendedUntil should still be set (cleanup hasn't run)")
	}
	t.Logf("SuspendedUntil: %v (expired)", retrieved.SuspendedUntil)
	
	// Now call Add() with updated device info (e.g., updated hostname)
	// This is a common operation - updating device metadata
	updatedDev := Device{
		IP:             "192.168.1.100",
		Hostname:       "updated-hostname",
		LastSeen:       time.Now(),
		SuspendedUntil: time.Time{}, // Not suspended
	}
	mgr.Add(updatedDev)
	
	// THE BUG: The counter should have been decremented because the device
	// transitioned from suspended (even if expired) to not suspended
	// But wasActivelySuspended = false (suspension expired)
	// So the decrement doesn't happen!
	
	// Check the counter using GetSuspendedCountAccurate (doesn't trigger cleanup)
	accurateCount := mgr.GetSuspendedCountAccurate()
	t.Logf("Accurate count: %d", accurateCount)
	
	// Load the cached counter directly
	cachedCount := int(mgr.suspendedCount.Load())
	t.Logf("Cached count (atomic): %d", cachedCount)
	
	// BUG: Cached count is still 1, but accurate count is 0!
	if cachedCount != accurateCount {
		t.Errorf("BUG FOUND: Cached count (%d) != Accurate count (%d)", cachedCount, accurateCount)
		t.Errorf("The counter failed to decrement when Add() updated a device with expired suspension")
	}
}
