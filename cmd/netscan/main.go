package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/marko/netscan/internal/config"
	"github.com/marko/netscan/internal/discovery"
	"github.com/marko/netscan/internal/influx"
	"github.com/marko/netscan/internal/monitoring"
	"github.com/marko/netscan/internal/state"
)

func main() {
	cfg, err := config.LoadConfig("config.yml")
	if err != nil {
		panic(err)
	}

	mgr := state.NewManager()
	writer := influx.NewWriter(cfg.InfluxDB.URL, cfg.InfluxDB.Token, cfg.InfluxDB.Org, cfg.InfluxDB.Bucket)
	defer writer.Close()

	activePingers := make(map[string]context.CancelFunc)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	discoveryTicker := time.NewTicker(cfg.DiscoveryInterval)
	defer discoveryTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			for _, cancel := range activePingers {
				cancel()
			}
			return
		case <-discoveryTicker.C:
			devices := discovery.RunScan(cfg)
			for _, dev := range devices {
				mgr.Add(dev)
				mgr.UpdateLastSeen(dev.IP)
				if _, running := activePingers[dev.IP]; !running {
					pingerCtx, cancel := context.WithCancel(ctx)
					activePingers[dev.IP] = cancel
					go monitoring.StartPinger(dev, cfg, writer, pingerCtx)
				}
			}
			pruned := mgr.Prune(cfg.DiscoveryInterval * 2)
			for _, old := range pruned {
				if cancel, ok := activePingers[old.IP]; ok {
					cancel()
					delete(activePingers, old.IP)
				}
			}
		}
	}
}
