package state

import (
"testing"
"time"
)

// TestSameDeviceMultipleSuspensions - reproduces the bug where the same device
// tripping the circuit breaker multiple times causes suspended_devices to increment
// even though only one device is actually suspended
func TestSameDeviceMultipleSuspensions(t *testing.T) {
mgr := NewManager(1000)

// Add a device
dev := Device{
IP:       "10.128.60.2",
Hostname: "test-device",
LastSeen: time.Now(),
}
mgr.Add(dev)

maxFails := 10
backoff := 5 * time.Minute

// Trip the circuit breaker 4 times for the SAME device
for i := 0; i < 4; i++ {
// First, fail enough times to trip the breaker
for j := 0; j < maxFails; j++ {
mgr.ReportPingFail("10.128.60.2", maxFails, backoff)
}

// Now the device is suspended
if !mgr.IsSuspended("10.128.60.2") {
t.Fatalf("Device should be suspended after iteration %d", i+1)
}

// Check the suspended count - it should ALWAYS be 1
suspendedCount := mgr.GetSuspendedCount()
t.Logf("After suspension %d: suspended_devices count = %d", i+1, suspendedCount)

// This is the bug: suspended_devices increments to 2, 3, 4 instead of staying at 1
if suspendedCount != 1 {
t.Errorf("After suspension %d: Expected suspended_devices = 1 (only one device), got %d", i+1, suspendedCount)
}
}

// Verify using accurate count
accurateCount := mgr.GetSuspendedCountAccurate()
t.Logf("Accurate count: %d", accurateCount)

if accurateCount != 1 {
t.Errorf("Accurate count should be 1, got %d", accurateCount)
}

// The atomic counter should match
cachedCount := mgr.GetSuspendedCount()
if cachedCount != accurateCount {
t.Errorf("Cached count (%d) doesn't match accurate count (%d)", cachedCount, accurateCount)
}
}
