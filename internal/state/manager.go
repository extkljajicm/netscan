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
	devices map[string]*Device // Map IP addresses to device pointers
	mu      sync.RWMutex       // Protects concurrent access to devices map
}

// NewManager creates a new device state manager
func NewManager() *Manager {
	return &Manager{
		devices: make(map[string]*Device),
	}
}

// Add inserts a new device if it doesn't already exist (idempotent operation)
func (m *Manager) Add(device Device) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.devices[device.IP]; !exists {
		m.devices[device.IP] = &device
	}
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
