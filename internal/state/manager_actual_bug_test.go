package state

import (
	"testing"
	"time"
)

// TestRealBug_CleanupBeforeRecovery - Tests the actual bug where GetSuspendedCount()
// cleans up an expired suspension, but then ReportPingSuccess() fails to decrement
// because SuspendedUntil was already cleared
func TestRealBug_CleanupBeforeRecovery(t *testing.T) {
	mgr := NewManager(1000)

	// Add a device
	dev := Device{
		IP:       "192.168.1.100",
		Hostname: "test-device",
		LastSeen: time.Now(),
	}
	mgr.Add(dev)

	// Trip the circuit breaker
	maxFails := 3
	backoff := 100 * time.Millisecond
	for i := 0; i < maxFails; i++ {
		mgr.ReportPingFail("192.168.1.100", maxFails, backoff)
	}

	// Verify counter is 1
	count1 := mgr.GetSuspendedCount()
	t.Logf("Count after suspension: %d", count1)
	if count1 != 1 {
		t.Fatalf("Expected count 1, got %d", count1)
	}

	// Wait for suspension to expire
	time.Sleep(backoff + 50*time.Millisecond)

	// Call GetSuspendedCount() which triggers cleanup
	// This should decrement counter to 0 and clear SuspendedUntil
	count2 := mgr.GetSuspendedCount()
	t.Logf("Count after cleanup: %d", count2)
	if count2 != 0 {
		t.Errorf("Expected count 0 after cleanup, got %d", count2)
	}

	// Check device state - SuspendedUntil should be cleared
	retrieved, _ := mgr.Get("192.168.1.100")
	if !retrieved.SuspendedUntil.IsZero() {
		t.Logf("UNEXPECTED: SuspendedUntil is %v (should be zero)", retrieved.SuspendedUntil)
	}

	// Now call ReportPingSuccess() - simulating device recovery
	// BUG: This won't decrement because SuspendedUntil was already cleared
	mgr.ReportPingSuccess("192.168.1.100")

	// Check counter - should still be 0
	count3 := mgr.GetSuspendedCount()
	t.Logf("Count after ReportPingSuccess: %d", count3)
	
	// This should pass - cleanup already decremented, so ReportPingSuccess
	// correctly does NOT decrement again (SuspendedUntil is zero)
	if count3 != 0 {
		t.Errorf("Expected count 0, got %d", count3)
	}
}

// TestRealBug_RecoveryBeforeCleanup - Tests what happens when ReportPingSuccess()
// is called BEFORE GetSuspendedCount() has a chance to clean up an expired suspension
func TestRealBug_RecoveryBeforeCleanup(t *testing.T) {
	mgr := NewManager(1000)

	// Add a device
	dev := Device{
		IP:       "192.168.1.200",
		Hostname: "test-device",
		LastSeen: time.Now(),
	}
	mgr.Add(dev)

	// Trip the circuit breaker
	maxFails := 3
	backoff := 100 * time.Millisecond
	for i := 0; i < maxFails; i++ {
		mgr.ReportPingFail("192.168.1.200", maxFails, backoff)
	}

	// Verify counter is 1
	count1 := mgr.GetSuspendedCount()
	t.Logf("Count after suspension: %d", count1)
	if count1 != 1 {
		t.Fatalf("Expected count 1, got %d", count1)
	}

	// Wait for suspension to expire
	time.Sleep(backoff + 50*time.Millisecond)

	// Now check if device is suspended - should return false
	if mgr.IsSuspended("192.168.1.200") {
		t.Error("Device should not be suspended after expiration")
	}

	// BUT - GetSuspendedCount() has NOT been called yet, so cleanup hasn't run
	// Check the device state directly
	retrieved, _ := mgr.Get("192.168.1.200")
	t.Logf("SuspendedUntil: %v", retrieved.SuspendedUntil)
	
	// SuspendedUntil is still set (not cleared), but it's in the past
	if retrieved.SuspendedUntil.IsZero() {
		t.Log("SuspendedUntil is zero (cleanup ran somehow)")
	} else {
		t.Log("SuspendedUntil is still set (cleanup hasn't run)")
	}

	// Now call ReportPingSuccess() BEFORE cleanup runs
	mgr.ReportPingSuccess("192.168.1.200")

	// Check counter
	count2 := mgr.GetSuspendedCount()
	t.Logf("Count after ReportPingSuccess: %d", count2)
	
	// Counter should be 0 (ReportPingSuccess decremented it)
	if count2 != 0 {
		t.Errorf("Expected count 0, got %d", count2)
	}
}
