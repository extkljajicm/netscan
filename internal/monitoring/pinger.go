package monitoring

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/extkljajicm/netscan/internal/state"
	probing "github.com/prometheus-community/pro-bing"
)

// PingWriter interface for writing ping results to external storage
type PingWriter interface {
	WritePingResult(ip string, rtt time.Duration, successful bool) error
	WriteDeviceInfo(ip, hostname, sysDescr string) error
}

// StateManager interface for updating device last seen timestamp
type StateManager interface {
	UpdateLastSeen(ip string)
}

// StartPinger runs continuous ICMP monitoring for a single device
func StartPinger(ctx context.Context, wg *sync.WaitGroup, device state.Device, interval time.Duration, writer PingWriter, stateMgr StateManager) {
	if wg != nil {
		defer wg.Done()
	}
	
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return // Exit on context cancellation
		case <-ticker.C:
			log.Printf("Pinging %s...", device.IP)

			// Validate IP address before pinging
			if err := validateIPAddress(device.IP); err != nil {
				log.Printf("Invalid IP address for %s: %v", device.IP, err)
				continue
			}

			pinger, err := probing.NewPinger(device.IP)
			if err != nil {
				log.Printf("Ping error for %s: %v", device.IP, err)
				continue // Skip invalid IP configurations
			}
			pinger.Count = 1                              // Single ICMP echo request per interval
			pinger.Timeout = 2 * time.Second              // 2-second ping timeout
			pinger.SetPrivileged(true)                    // Use raw ICMP sockets (requires root)
			if err := pinger.Run(); err != nil {
				log.Printf("Ping run error for %s: %v", device.IP, err)
				continue // Skip execution errors
			}
			stats := pinger.Statistics()
			if stats.PacketsRecv > 0 {
				log.Printf("Ping success: %s RTT=%v", device.IP, stats.AvgRtt)
				// Update last seen timestamp in state manager
				if stateMgr != nil {
					stateMgr.UpdateLastSeen(device.IP)
				}
				if err := writer.WritePingResult(device.IP, stats.AvgRtt, true); err != nil {
					log.Printf("Failed to write ping result for %s: %v", device.IP, err)
				}
			} else {
				log.Printf("Ping failed: %s", device.IP)
				if err := writer.WritePingResult(device.IP, 0, false); err != nil {
					log.Printf("Failed to write ping failure for %s: %v", device.IP, err)
				}
			}
		}
	}
}

// validateIPAddress validates IP address format and security constraints
func validateIPAddress(ipStr string) error {
	if ipStr == "" {
		return fmt.Errorf("IP address cannot be empty")
	}

	ip := net.ParseIP(ipStr)
	if ip == nil {
		return fmt.Errorf("invalid IP address format: %s", ipStr)
	}

	// Security checks - prevent pinging dangerous addresses
	if ip.IsLoopback() {
		return fmt.Errorf("loopback addresses not allowed: %s", ipStr)
	}
	if ip.IsMulticast() {
		return fmt.Errorf("multicast addresses not allowed: %s", ipStr)
	}
	if ip.IsLinkLocalUnicast() {
		return fmt.Errorf("link-local addresses not allowed: %s", ipStr)
	}
	if ip.IsUnspecified() {
		return fmt.Errorf("unspecified addresses not allowed: %s", ipStr)
	}

	// Additional validation for IPv4 addresses
	if ip.To4() != nil {
		// Note: We allow .0 and .255 addresses as they may be valid device IPs in some networks
		// The security checks above (loopback, multicast, etc.) are more important
	}

	return nil
}
