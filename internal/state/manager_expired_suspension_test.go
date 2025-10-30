package state

import (
"testing"
"time"
)

// TestExpiredSuspensionStuckCounter - reproduces the bug where expired suspensions
// don't automatically clear from the counter
func TestExpiredSuspensionStuckCounter(t *testing.T) {
mgr := NewManager(1000)

// Add a device
dev := Device{
IP:       "10.0.0.1",
Hostname: "test-device",
LastSeen: time.Now(),
}
mgr.Add(dev)

maxFails := 3
backoff := 100 * time.Millisecond // Very short backoff for testing

// Trip the circuit breaker
for i := 0; i < maxFails; i++ {
mgr.ReportPingFail("10.0.0.1", maxFails, backoff)
}

// Device should be suspended
if !mgr.IsSuspended("10.0.0.1") {
t.Fatal("Device should be suspended")
}

// Counter should be 1
if count := mgr.GetSuspendedCount(); count != 1 {
t.Fatalf("Expected suspended count = 1, got %d", count)
}

// Wait for suspension to expire
time.Sleep(150 * time.Millisecond)

// Device should no longer be suspended
if mgr.IsSuspended("10.0.0.1") {
t.Fatal("Device should NOT be suspended anymore (suspension expired)")
}

// But the counter is STUCK at 1 because ReportPingSuccess was never called
cachedCount := mgr.GetSuspendedCount()
accurateCount := mgr.GetSuspendedCountAccurate()

t.Logf("Cached count: %d, Accurate count: %d", cachedCount, accurateCount)

// THIS IS THE BUG: cached counter is stuck at 1, but accurate count is 0
if cachedCount == accurateCount {
t.Logf("SUCCESS: Cached count matches accurate count (both %d)", cachedCount)
} else {
t.Errorf("BUG REPRODUCED: Cached count (%d) != Accurate count (%d)", cachedCount, accurateCount)
t.Errorf("The suspended_devices counter is stuck at %d even though suspension expired", cachedCount)
}
}
