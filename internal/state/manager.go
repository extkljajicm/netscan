package state

import (
	"sync"
	"time"
)

type Device struct {
	IP          string
	Hostname    string
	SysDescr    string
	SysObjectID string
	LastSeen    time.Time
}

type Manager struct {
	devices map[string]*Device
	mu      sync.RWMutex
}

func NewManager() *Manager {
	return &Manager{
		devices: make(map[string]*Device),
	}
}

func (m *Manager) Add(device Device) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.devices[device.IP]; !exists {
		m.devices[device.IP] = &device
	}
}

func (m *Manager) Get(ip string) (*Device, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	dev, exists := m.devices[ip]
	return dev, exists
}

func (m *Manager) GetAll() []Device {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]Device, 0, len(m.devices))
	for _, dev := range m.devices {
		result = append(result, *dev)
	}
	return result
}

func (m *Manager) UpdateLastSeen(ip string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if dev, exists := m.devices[ip]; exists {
		dev.LastSeen = time.Now()
	}
}

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
