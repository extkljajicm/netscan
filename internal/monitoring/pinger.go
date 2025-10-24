package monitoring

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/kljama/netscan/internal/state"
	probing "github.com/prometheus-community/pro-bing"
	"github.com/rs/zerolog/log"
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
	// Panic recovery for pinger goroutine
	defer func() {
		if r := recover(); r != nil {
			log.Error().
				Str("ip", device.IP).
				Interface("panic", r).
				Msg("Pinger panic recovered")
		}
	}()

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
			log.Debug().Str("ip", device.IP).Msg("Pinging device")

			// Validate IP address before pinging
			if err := validateIPAddress(device.IP); err != nil {
				log.Error().
					Str("ip", device.IP).
					Err(err).
					Msg("Invalid IP address")
				continue
			}

			pinger, err := probing.NewPinger(device.IP)
			if err != nil {
				log.Error().
					Str("ip", device.IP).
					Err(err).
					Msg("Failed to create pinger")
				continue // Skip invalid IP configurations
			}
			pinger.Count = 1                              // Single ICMP echo request per interval
			pinger.Timeout = 2 * time.Second              // 2-second ping timeout
			pinger.SetPrivileged(true)                    // Use raw ICMP sockets (requires root)
			if err := pinger.Run(); err != nil {
				log.Error().
					Str("ip", device.IP).
					Err(err).
					Msg("Ping execution failed")
				continue // Skip execution errors
			}
			stats := pinger.Statistics()
			if stats.PacketsRecv > 0 {
				log.Debug().
					Str("ip", device.IP).
					Dur("rtt", stats.AvgRtt).
					Msg("Ping successful")
				// Update last seen timestamp in state manager
				if stateMgr != nil {
					stateMgr.UpdateLastSeen(device.IP)
				}
				if err := writer.WritePingResult(device.IP, stats.AvgRtt, true); err != nil {
					log.Error().
						Str("ip", device.IP).
						Err(err).
						Msg("Failed to write ping result")
				}
			} else {
				log.Debug().
					Str("ip", device.IP).
					Msg("Ping failed - no response")
				if err := writer.WritePingResult(device.IP, 0, false); err != nil {
					log.Error().
						Str("ip", device.IP).
						Err(err).
						Msg("Failed to write ping failure")
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
