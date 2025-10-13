package monitoring

import (
	"context"
	"time"

	"github.com/go-ping/ping"
	"github.com/marko/netscan/internal/config"
	"github.com/marko/netscan/internal/influx"
	"github.com/marko/netscan/internal/state"
)

func StartPinger(device state.Device, cfg *config.Config, writer *influx.Writer, ctx context.Context) {
	ticker := time.NewTicker(cfg.PingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pinger, err := ping.NewPinger(device.IP)
			if err != nil {
				writer.WritePingResult(device.IP, device.Hostname, 0, false)
				continue
			}
			pinger.Count = 1
			pinger.Timeout = cfg.PingTimeout
			pinger.SetPrivileged(true)
			if err := pinger.Run(); err != nil {
				writer.WritePingResult(device.IP, device.Hostname, 0, false)
				continue
			}
			stats := pinger.Statistics()
			writer.WritePingResult(device.IP, device.Hostname, stats.AvgRtt, stats.PacketsRecv > 0)
		}
	}
}
