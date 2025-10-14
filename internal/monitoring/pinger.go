package monitoring

import (
	"context"
	"log"
	"time"

	"github.com/extkljajicm/netscan/internal/config"
	"github.com/extkljajicm/netscan/internal/state"
	probing "github.com/prometheus-community/pro-bing"
)

// PingWriter interface for writing ping results to external storage
type PingWriter interface {
	WritePingResult(ip string, rtt time.Duration, successful bool) error
	WriteDeviceInfo(ip, hostname, sysName, sysDescr, sysObjectID string) error
}

// StartPinger runs continuous ICMP monitoring for a single device
func StartPinger(device state.Device, cfg *config.Config, writer PingWriter, ctx context.Context) {
	ticker := time.NewTicker(cfg.PingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return // Exit on context cancellation
		case <-ticker.C:
			log.Printf("Pinging %s...", device.IP)
			pinger, err := probing.NewPinger(device.IP)
			if err != nil {
				log.Printf("Ping error for %s: %v", device.IP, err)
				continue // Skip invalid IP configurations
			}
			pinger.Count = 1                 // Single ICMP echo request per interval
			pinger.Timeout = cfg.PingTimeout // Configured ping timeout
			pinger.SetPrivileged(true)       // Use raw ICMP sockets (requires root)
			if err := pinger.Run(); err != nil {
				log.Printf("Ping run error for %s: %v", device.IP, err)
				continue // Skip execution errors
			}
			stats := pinger.Statistics()
			if stats.PacketsRecv > 0 {
				log.Printf("Ping success: %s RTT=%v", device.IP, stats.AvgRtt)
				writer.WritePingResult(device.IP, stats.AvgRtt, true)
			} else {
				log.Printf("Ping failed: %s", device.IP)
				writer.WritePingResult(device.IP, 0, false)
			}
		}
	}
}
