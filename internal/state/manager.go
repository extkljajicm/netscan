package state

import (
	"sync"
	"time"
)

// Device represents a discovered network device with metadata
type Device struct {
	IP          string    // IPv4 address of the device
	Hostname    string    // Device hostname from SNMP or IP address
	SysDescr    string    // SNMP sysDescr MIB-II value
	SysObjectID string    // SNMP sysObjectID MIB-II value
	LastSeen    time.Time // Timestamp of last successful discovery
}

// Manager provides thread-safe device state management
type Manager struct {
	devices    map[string]*Device // Map IP addresses to device pointers
	mu         sync.RWMutex       // Protects concurrent access to devices map
	maxDevices int                // Maximum number of devices to manage
}

// NewManager creates a new device state manager
func NewManager(maxDevices int) *Manager {
	if maxDevices <= 0 {
		maxDevices = 10000 // Default if not specified
	}
	return &Manager{
		devices:    make(map[string]*Device),
		maxDevices: maxDevices,
	}
}

// Add inserts a new device if it doesn't already exist (idempotent operation)
// Enforces device count limits by removing oldest devices when limit is reached
func (m *Manager) Add(device Device) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// If device already exists, just update it
	if existing, exists := m.devices[device.IP]; exists {
		*existing = device
		return
	}

	// Check if we've reached the device limit
	if len(m.devices) >= m.maxDevices {
		// Remove the oldest device (smallest LastSeen time)
		var oldestIP string
		var oldestTime time.Time
		first := true
		for ip, dev := range m.devices {
			if first || dev.LastSeen.Before(oldestTime) {
				oldestIP = ip
				oldestTime = dev.LastSeen
				first = false
			}
		}
		if oldestIP != "" {
			delete(m.devices, oldestIP)
		}
	}

	// Add the new device
	m.devices[device.IP] = &device
}

// AddDevice adds a device by IP address only, returns true if it's a new device
func (m *Manager) AddDevice(ip string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	// If device already exists, return false
	if _, exists := m.devices[ip]; exists {
		return false
	}

	// Check if we've reached the device limit
	if len(m.devices) >= m.maxDevices {
		// Remove the oldest device (smallest LastSeen time)
		var oldestIP string
		var oldestTime time.Time
		first := true
		for devIP, dev := range m.devices {
			if first || dev.LastSeen.Before(oldestTime) {
				oldestIP = devIP
				oldestTime = dev.LastSeen
				first = false
			}
		}
		if oldestIP != "" {
			delete(m.devices, oldestIP)
		}
	}

	// Add the new device with minimal info
	m.devices[ip] = &Device{
		IP:       ip,
		Hostname: ip,
		LastSeen: time.Now(),
	}
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
func (m *Manager) UpdateLastSeen(ip string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if dev, exists := m.devices[ip]; exists {
		dev.LastSeen = time.Now()
	}
}

// UpdateDeviceSNMP enriches an existing device with SNMP data
func (m *Manager) UpdateDeviceSNMP(ip, hostname, sysDescr, sysObjectID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if dev, exists := m.devices[ip]; exists {
		dev.Hostname = hostname
		dev.SysDescr = sysDescr
		dev.SysObjectID = sysObjectID
		dev.LastSeen = time.Now()
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
func (m *Manager) Prune(olderThan time.Duration) []Device {
	m.mu.Lock()
	defer m.mu.Unlock()
	var removed []Device
	cutoff := time.Now().Add(-olderThan)
	for ip, dev := range m.devices {
		if dev.LastSeen.Before(cutoff) {
			removed = append(removed, *dev)
			delete(m.devices, ip)
		}
	}
	return removed
}

// PruneStale is an alias for Prune with a clearer name for the new architecture
func (m *Manager) PruneStale(olderThan time.Duration) []Device {
	return m.Prune(olderThan)
}
