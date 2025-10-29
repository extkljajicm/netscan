package state

import (
	"container/heap"
	"sync"
	"sync/atomic"
	"time"
)

// Device represents a discovered network device with metadata
type Device struct {
	IP                     string    // IPv4 address of the device
	Hostname               string    // Device hostname from SNMP or IP address
	SysDescr               string    // SNMP sysDescr MIB-II value
	LastSeen               time.Time // Timestamp of last successful discovery
	ConsecutiveFails       int       // Number of consecutive ping failures (circuit breaker)
	SuspendedUntil         time.Time // Timestamp until which device is suspended (circuit breaker)
	SNMPConsecutiveFails   int       // Number of consecutive SNMP failures (SNMP circuit breaker)
	SNMPSuspendedUntil     time.Time // Timestamp until which SNMP polling is suspended (SNMP circuit breaker)
	heapIndex              int       // Index in the min-heap for O(log n) eviction (internal use only)
}

// deviceHeap implements heap.Interface for min-heap ordered by LastSeen timestamp
// This enables O(log n) LRU eviction instead of O(n) iteration
type deviceHeap []*Device

func (h deviceHeap) Len() int { return len(h) }

func (h deviceHeap) Less(i, j int) bool {
	// Min-heap: oldest (smallest LastSeen) at top
	return h[i].LastSeen.Before(h[j].LastSeen)
}

func (h deviceHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].heapIndex = i
	h[j].heapIndex = j
}

func (h *deviceHeap) Push(x interface{}) {
	n := len(*h)
	device := x.(*Device)
	device.heapIndex = n
	*h = append(*h, device)
}

func (h *deviceHeap) Pop() interface{} {
	old := *h
	n := len(old)
	device := old[n-1]
	old[n-1] = nil         // Avoid memory leak
	device.heapIndex = -1  // Mark as not in heap
	*h = old[0 : n-1]
	return device
}

// Manager provides thread-safe device state management with O(log n) LRU eviction
type Manager struct {
	devices             map[string]*Device // Map IP addresses to device pointers
	evictionHeap        deviceHeap         // Min-heap for O(log n) LRU eviction
	mu                  sync.RWMutex       // Protects concurrent access to devices map and heap
	maxDevices          int                // Maximum number of devices to manage
	suspendedCount      atomic.Int32       // Cached count of ping-suspended devices (for O(1) reads)
	snmpSuspendedCount  atomic.Int32       // Cached count of SNMP-suspended devices (for O(1) reads)
}

// NewManager creates a new device state manager with heap-based LRU eviction
func NewManager(maxDevices int) *Manager {
	if maxDevices <= 0 {
		maxDevices = 10000 // Default if not specified
	}
	m := &Manager{
		devices:      make(map[string]*Device),
		evictionHeap: make(deviceHeap, 0, maxDevices),
		maxDevices:   maxDevices,
	}
	heap.Init(&m.evictionHeap)
	return m
}

// Add inserts a new device if it doesn't already exist (idempotent operation)
// Enforces device count limits by removing oldest devices when limit is reached
// Uses min-heap for O(log n) eviction instead of O(n) iteration
func (m *Manager) Add(device Device) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// If device already exists, update it
	if existing, exists := m.devices[device.IP]; exists {
		// Track suspension state changes for atomic counter
		wasActivelySuspended := !existing.SuspendedUntil.IsZero() && time.Now().Before(existing.SuspendedUntil)
		willBeActivelySuspended := !device.SuspendedUntil.IsZero() && time.Now().Before(device.SuspendedUntil)
		
		// Track SNMP suspension state changes for atomic counter
		wasActivelySNMPSuspended := !existing.SNMPSuspendedUntil.IsZero() && time.Now().Before(existing.SNMPSuspendedUntil)
		willBeActivelySNMPSuspended := !device.SNMPSuspendedUntil.IsZero() && time.Now().Before(device.SNMPSuspendedUntil)
		
		// Update the atomic counter based on state change
		if !wasActivelySuspended && willBeActivelySuspended {
			m.suspendedCount.Add(1) // Device became suspended
		} else if wasActivelySuspended && !willBeActivelySuspended {
			m.suspendedCount.Add(-1) // Device no longer suspended
		}
		
		// Update the SNMP atomic counter based on state change
		if !wasActivelySNMPSuspended && willBeActivelySNMPSuspended {
			m.snmpSuspendedCount.Add(1) // SNMP polling became suspended
		} else if wasActivelySNMPSuspended && !willBeActivelySNMPSuspended {
			m.snmpSuspendedCount.Add(-1) // SNMP polling no longer suspended
		}
		
		// Update device fields
		oldLastSeen := existing.LastSeen
		*existing = device
		
		// If LastSeen changed, update heap position (O(log n))
		if !device.LastSeen.Equal(oldLastSeen) && existing.heapIndex >= 0 {
			heap.Fix(&m.evictionHeap, existing.heapIndex)
		}
		return
	}

	// Check if we've reached the device limit
	if len(m.devices) >= m.maxDevices {
		// Remove the oldest device using heap (O(log n) instead of O(n))
		if m.evictionHeap.Len() > 0 {
			oldest := heap.Pop(&m.evictionHeap).(*Device)
			
			// If the device being evicted was ping-suspended, decrement counter
			if !oldest.SuspendedUntil.IsZero() && time.Now().Before(oldest.SuspendedUntil) {
				m.suspendedCount.Add(-1)
			}
			
			// If the device being evicted had SNMP suspended, decrement counter
			if !oldest.SNMPSuspendedUntil.IsZero() && time.Now().Before(oldest.SNMPSuspendedUntil) {
				m.snmpSuspendedCount.Add(-1)
			}
			
			delete(m.devices, oldest.IP)
		}
	}

	// Add the new device
	// If the new device is actively ping-suspended, increment the counter
	if !device.SuspendedUntil.IsZero() && time.Now().Before(device.SuspendedUntil) {
		m.suspendedCount.Add(1)
	}
	
	// If the new device has SNMP actively suspended, increment the counter
	if !device.SNMPSuspendedUntil.IsZero() && time.Now().Before(device.SNMPSuspendedUntil) {
		m.snmpSuspendedCount.Add(1)
	}
	
	devicePtr := &device
	m.devices[device.IP] = devicePtr
	heap.Push(&m.evictionHeap, devicePtr)
}

// AddDevice adds a device by IP address only, returns true if it's a new device
// Uses min-heap for O(log n) eviction instead of O(n) iteration
func (m *Manager) AddDevice(ip string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	// If device already exists, return false
	if _, exists := m.devices[ip]; exists {
		return false
	}

	// Check if we've reached the device limit
	if len(m.devices) >= m.maxDevices {
		// Remove the oldest device using heap (O(log n) instead of O(n))
		if m.evictionHeap.Len() > 0 {
			oldest := heap.Pop(&m.evictionHeap).(*Device)
			
			// If the device being evicted was ping-suspended, decrement counter
			if !oldest.SuspendedUntil.IsZero() && time.Now().Before(oldest.SuspendedUntil) {
				m.suspendedCount.Add(-1)
			}
			
			// If the device being evicted had SNMP suspended, decrement counter
			if !oldest.SNMPSuspendedUntil.IsZero() && time.Now().Before(oldest.SNMPSuspendedUntil) {
				m.snmpSuspendedCount.Add(-1)
			}
			
			delete(m.devices, oldest.IP)
		}
	}

	// Add the new device with minimal info
	device := &Device{
		IP:       ip,
		Hostname: ip,
		LastSeen: time.Now(),
	}
	m.devices[ip] = device
	heap.Push(&m.evictionHeap, device)
	return true
}

// Get retrieves a device by IP address, returns nil if not found
func (m *Manager) Get(ip string) (*Device, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	dev, exists := m.devices[ip]
	return dev, exists
}

// GetAll returns a copy of all managed devices
func (m *Manager) GetAll() []Device {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]Device, 0, len(m.devices))
	for _, dev := range m.devices {
		result = append(result, *dev)
	}
	return result
}

// UpdateLastSeen refreshes the LastSeen timestamp for an existing device
// Updates heap position to maintain LRU ordering (O(log n))
func (m *Manager) UpdateLastSeen(ip string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if dev, exists := m.devices[ip]; exists {
		dev.LastSeen = time.Now()
		// Update heap position since LastSeen changed (O(log n))
		if dev.heapIndex >= 0 {
			heap.Fix(&m.evictionHeap, dev.heapIndex)
		}
	}
}

// UpdateDeviceSNMP enriches an existing device with SNMP data
// Updates heap position since LastSeen changes (O(log n))
func (m *Manager) UpdateDeviceSNMP(ip, hostname, sysDescr string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if dev, exists := m.devices[ip]; exists {
		dev.Hostname = hostname
		dev.SysDescr = sysDescr
		dev.LastSeen = time.Now()
		// Update heap position since LastSeen changed (O(log n))
		if dev.heapIndex >= 0 {
			heap.Fix(&m.evictionHeap, dev.heapIndex)
		}
	}
}

// GetAllIPs returns a slice of all managed device IP addresses
func (m *Manager) GetAllIPs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ips := make([]string, 0, len(m.devices))
	for ip := range m.devices {
		ips = append(ips, ip)
	}
	return ips
}

// Prune removes devices not seen within the specified duration
// Removes devices from both the map and heap
func (m *Manager) Prune(olderThan time.Duration) []Device {
	m.mu.Lock()
	defer m.mu.Unlock()
	var removed []Device
	cutoff := time.Now().Add(-olderThan)
	now := time.Now()
	
	// Collect devices to remove
	var toRemove []*Device
	for ip, dev := range m.devices {
		if dev.LastSeen.Before(cutoff) {
			removed = append(removed, *dev)
			toRemove = append(toRemove, dev)
			
			// If the pruned device was actively ping-suspended, decrement counter
			if !dev.SuspendedUntil.IsZero() && now.Before(dev.SuspendedUntil) {
				m.suspendedCount.Add(-1)
			}
			
			// If the pruned device had SNMP actively suspended, decrement counter
			if !dev.SNMPSuspendedUntil.IsZero() && now.Before(dev.SNMPSuspendedUntil) {
				m.snmpSuspendedCount.Add(-1)
			}
			
			delete(m.devices, ip)
		}
	}
	
	// Remove from heap - rebuild is more efficient for bulk removals
	if len(toRemove) > 0 {
		// Build new heap excluding removed devices
		newHeap := make(deviceHeap, 0, len(m.evictionHeap)-len(toRemove))
		for _, dev := range m.evictionHeap {
			if _, exists := m.devices[dev.IP]; exists {
				newHeap = append(newHeap, dev)
			}
		}
		m.evictionHeap = newHeap
		heap.Init(&m.evictionHeap)
	}
	
	return removed
}

// PruneStale is an alias for Prune with a clearer name for the new architecture
func (m *Manager) PruneStale(olderThan time.Duration) []Device {
	return m.Prune(olderThan)
}

// Count returns the current number of managed devices
func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.devices)
}

// ReportPingSuccess resets circuit breaker state on successful ping
func (m *Manager) ReportPingSuccess(ip string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if dev, exists := m.devices[ip]; exists {
		// If SuspendedUntil is set (device was suspended at some point), decrement counter
		// This handles both active suspensions and expired ones
		if !dev.SuspendedUntil.IsZero() {
			m.suspendedCount.Add(-1)
		}
		dev.ConsecutiveFails = 0
		dev.SuspendedUntil = time.Time{} // Zero time (not suspended)
	}
}

// ReportPingFail increments failure count and suspends device if threshold reached
// Returns true if the device was suspended (circuit breaker tripped)
func (m *Manager) ReportPingFail(ip string, maxFails int, backoff time.Duration) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	dev, exists := m.devices[ip]
	if !exists {
		return false
	}

	dev.ConsecutiveFails++
	
	// Check if we've reached the threshold
	if dev.ConsecutiveFails >= maxFails {
		// Trip the circuit breaker
		dev.ConsecutiveFails = 0 // Reset counter
		dev.SuspendedUntil = time.Now().Add(backoff)
		m.suspendedCount.Add(1) // Increment atomic counter
		return true // Device is now suspended
	}
	
	return false // Device not suspended
}

// IsSuspended checks if a device is currently suspended by the circuit breaker
func (m *Manager) IsSuspended(ip string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	dev, exists := m.devices[ip]
	if !exists {
		return false
	}
	
	// Device is suspended if SuspendedUntil is set and in the future
	return !dev.SuspendedUntil.IsZero() && time.Now().Before(dev.SuspendedUntil)
}

// GetSuspendedCount returns the number of currently suspended devices
// This uses a cached atomic counter for O(1) performance (optimized for frequent calls)
// Note: The count may be slightly stale if suspensions have expired but haven't been cleared yet
func (m *Manager) GetSuspendedCount() int {
	return int(m.suspendedCount.Load())
}

// GetSuspendedCountAccurate returns an accurate count by iterating all devices
// This is O(n) but provides the most up-to-date count including expired suspensions
// Only use when accuracy is critical (e.g., debugging)
func (m *Manager) GetSuspendedCountAccurate() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	count := 0
	now := time.Now()
	for _, dev := range m.devices {
		// Use the same logic as IsSuspended
		if !dev.SuspendedUntil.IsZero() && now.Before(dev.SuspendedUntil) {
			count++
		}
	}
	return count
}

// ReportSNMPSuccess resets SNMP circuit breaker state on successful SNMP query
func (m *Manager) ReportSNMPSuccess(ip string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if dev, exists := m.devices[ip]; exists {
		// If SNMPSuspendedUntil is set (device was suspended at some point), decrement counter
		// This handles both active suspensions and expired ones
		if !dev.SNMPSuspendedUntil.IsZero() {
			m.snmpSuspendedCount.Add(-1)
		}
		dev.SNMPConsecutiveFails = 0
		dev.SNMPSuspendedUntil = time.Time{} // Zero time (not suspended)
	}
}

// ReportSNMPFail increments SNMP failure count and suspends SNMP polling if threshold reached
// Returns true if SNMP polling was suspended (circuit breaker tripped)
func (m *Manager) ReportSNMPFail(ip string, maxFails int, backoff time.Duration) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	dev, exists := m.devices[ip]
	if !exists {
		return false
	}

	dev.SNMPConsecutiveFails++
	
	// Check if we've reached the threshold
	if dev.SNMPConsecutiveFails >= maxFails {
		// Trip the circuit breaker
		dev.SNMPConsecutiveFails = 0 // Reset counter
		dev.SNMPSuspendedUntil = time.Now().Add(backoff)
		m.snmpSuspendedCount.Add(1) // Increment atomic counter
		return true // SNMP polling is now suspended
	}
	
	return false // SNMP polling not suspended
}

// IsSNMPSuspended checks if SNMP polling is currently suspended by the circuit breaker
func (m *Manager) IsSNMPSuspended(ip string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	dev, exists := m.devices[ip]
	if !exists {
		return false
	}
	
	// SNMP polling is suspended if SNMPSuspendedUntil is set and in the future
	return !dev.SNMPSuspendedUntil.IsZero() && time.Now().Before(dev.SNMPSuspendedUntil)
}

// GetSNMPSuspendedCount returns the number of devices with SNMP polling currently suspended
// This uses a cached atomic counter for O(1) performance (optimized for frequent calls)
// Note: The count may be slightly stale if suspensions have expired but haven't been cleared yet
func (m *Manager) GetSNMPSuspendedCount() int {
	return int(m.snmpSuspendedCount.Load())
}
