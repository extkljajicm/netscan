package state

import (
	"testing"
	"time"
)

// TestRecoveryAfterExpiredSuspensionBug - Tests that the suspended_devices counter
// is correctly decremented when a device with an EXPIRED suspension reports success
func TestRecoveryAfterExpiredSuspensionBug(t *testing.T) {
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

	// Verify device is suspended and counter is 1
	if !mgr.IsSuspended("192.168.1.100") {
		t.Fatal("Device should be suspended")
	}
	if count := mgr.GetSuspendedCount(); count != 1 {
		t.Errorf("Expected suspended count 1, got %d", count)
	}

	// Wait for suspension to expire
	time.Sleep(backoff + 50*time.Millisecond)

	// Now suspension has expired
	if mgr.IsSuspended("192.168.1.100") {
		t.Error("Device should not be suspended after expiration")
	}

	// Check the count - after GetSuspendedCount() is called, it should clean up expired suspensions
	countAfterExpiry := mgr.GetSuspendedCount()
	t.Logf("Count after suspension expired: %d", countAfterExpiry)

	// Now report a successful ping - this is the recovery path
	mgr.ReportPingSuccess("192.168.1.100")

	// Check the count again
	countAfterRecovery := mgr.GetSuspendedCount()
	t.Logf("Count after recovery: %d", countAfterRecovery)

	// The count should be 0, not negative!
	if countAfterRecovery != 0 {
		t.Errorf("Expected suspended count 0 after recovery, got %d", countAfterRecovery)
	}

	// Verify using accurate count
	accurateCount := mgr.GetSuspendedCountAccurate()
	if accurateCount != 0 {
		t.Errorf("Accurate count should be 0, got %d", accurateCount)
	}
}

// TestPruneRemovesSuspendedDeviceBug - Tests that the suspended_devices counter
// is correctly decremented when a suspended device is pruned
func TestPruneRemovesSuspendedDeviceBug(t *testing.T) {
	mgr := NewManager(1000)

	// Add a device
	dev := Device{
		IP:       "192.168.1.200",
		Hostname: "stale-device",
		LastSeen: time.Now().Add(-25 * time.Hour), // Very old
	}
	mgr.Add(dev)

	// Trip the circuit breaker
	maxFails := 3
	backoff := 1 * time.Hour
	for i := 0; i < maxFails; i++ {
		mgr.ReportPingFail("192.168.1.200", maxFails, backoff)
	}

	// Verify device is suspended and counter is 1
	if !mgr.IsSuspended("192.168.1.200") {
		t.Fatal("Device should be suspended")
	}
	countBeforePrune := mgr.GetSuspendedCount()
	t.Logf("Count before pruning: %d", countBeforePrune)
	if countBeforePrune != 1 {
		t.Errorf("Expected suspended count 1 before pruning, got %d", countBeforePrune)
	}

	// Prune stale devices (devices not seen in 24 hours)
	pruned := mgr.PruneStale(24 * time.Hour)
	t.Logf("Pruned %d devices", len(pruned))

	if len(pruned) != 1 {
		t.Errorf("Expected to prune 1 device, pruned %d", len(pruned))
	}

	// After pruning, the suspended device should be gone
	if mgr.Count() != 0 {
		t.Errorf("Expected 0 devices after pruning, got %d", mgr.Count())
	}

	// The suspended count should be 0 now
	countAfterPrune := mgr.GetSuspendedCount()
	t.Logf("Count after pruning: %d", countAfterPrune)
	if countAfterPrune != 0 {
		t.Errorf("Expected suspended count 0 after pruning suspended device, got %d", countAfterPrune)
	}

	// Verify using accurate count
	accurateCount := mgr.GetSuspendedCountAccurate()
	if accurateCount != 0 {
		t.Errorf("Accurate count should be 0, got %d", accurateCount)
	}
}
