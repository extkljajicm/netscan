package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/extkljajicm/netscan/internal/config"
	"github.com/extkljajicm/netscan/internal/discovery"
	"github.com/extkljajicm/netscan/internal/influx"
	"github.com/extkljajicm/netscan/internal/monitoring"
	"github.com/extkljajicm/netscan/internal/state"
)

func main() {
	icmpOnly := flag.Bool("icmp-only", false, "Skip SNMP discovery and only run ICMP pingers for configured IPs")
	flag.Parse()

	log.Println("netscan starting up...")
	cfg, err := config.LoadConfig("config.yml")
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	mgr := state.NewManager()
	writer := influx.NewWriter(cfg.InfluxDB.URL, cfg.InfluxDB.Token, cfg.InfluxDB.Org, cfg.InfluxDB.Bucket)
	defer writer.Close()

	// Map IP addresses to their pinger cancellation functions for cleanup
	activePingers := make(map[string]context.CancelFunc)

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	go func() {
		<-sigChan
		log.Println("Shutdown signal received. Stopping pingers and exiting.")
		stop()
	}()

	if *icmpOnly {
		log.Printf("--icmp-only flag set: Running ping-based discovery every %v, pinging online devices every %v...", cfg.IcmpDiscoveryInterval, cfg.PingInterval)
		// Execute initial ICMP discovery scan at startup
		log.Println("Running initial ping discovery scan...")
		devices := discovery.RunPingDiscovery(cfg.Networks[0], cfg.IcmpWorkers) // Assume single network
		log.Printf("Initial ping discovery found %d online devices.", len(devices))
		for _, dev := range devices {
			log.Printf("Online device: %s", dev.IP)
			mgr.Add(dev)
			mgr.UpdateLastSeen(dev.IP)
			if _, running := activePingers[dev.IP]; !running {
				log.Printf("Starting pinger for %s", dev.IP)
				pingerCtx, cancel := context.WithCancel(ctx)
				activePingers[dev.IP] = cancel
				go monitoring.StartPinger(dev, cfg, writer, pingerCtx)
			}
		}

		// Schedule periodic ICMP discovery using configured interval
		discoveryTicker := time.NewTicker(cfg.IcmpDiscoveryInterval)
		defer discoveryTicker.Stop()
		for {
			select {
			case <-ctx.Done():
				log.Println("Shutdown signal received. Stopping pingers and exiting.")
				for _, cancel := range activePingers {
					cancel()
				}
				return
			case <-discoveryTicker.C:
				log.Println("Running ping discovery scan...")
				devices := discovery.RunPingDiscovery(cfg.Networks[0], cfg.IcmpWorkers) // Assume single network
				log.Printf("Ping discovery found %d online devices.", len(devices))
				for _, dev := range devices {
					log.Printf("Online device: %s", dev.IP)
					mgr.Add(dev)
					mgr.UpdateLastSeen(dev.IP)
					if _, running := activePingers[dev.IP]; !running {
						log.Printf("Starting pinger for %s", dev.IP)
						pingerCtx, cancel := context.WithCancel(ctx)
						activePingers[dev.IP] = cancel
						go monitoring.StartPinger(dev, cfg, writer, pingerCtx)
					}
				}
				// Remove devices not seen in last 2 discovery cycles
				pruned := mgr.Prune(2 * cfg.IcmpDiscoveryInterval)
				for _, old := range pruned {
					log.Printf("Pruning offline device: %s", old.IP)
					if cancel, ok := activePingers[old.IP]; ok {
						cancel()
						delete(activePingers, old.IP)
					}
				}
			}
		}
	}

	// Execute initial full discovery scan at startup (ICMP first, then SNMP)
	log.Println("Running initial full discovery scan...")
	devices := discovery.RunFullDiscovery(cfg)
	log.Printf("Initial discovery found %d devices.", len(devices))
	for _, dev := range devices {
		log.Printf("Device found: %s (%s)", dev.IP, dev.Hostname)
		mgr.Add(dev)
		mgr.UpdateLastSeen(dev.IP)
		// Write device info to InfluxDB (only when discovered/changed)
		if err := writer.WriteDeviceInfo(dev.IP, dev.Hostname, dev.Hostname, dev.SysDescr, dev.SysObjectID); err != nil {
			log.Printf("Failed to write device info for %s: %v", dev.IP, err)
		}
		if _, running := activePingers[dev.IP]; !running {
			log.Printf("Starting pinger for %s (%s)", dev.IP, dev.Hostname)
			pingerCtx, cancel := context.WithCancel(ctx)
			activePingers[dev.IP] = cancel
			go monitoring.StartPinger(dev, cfg, writer, pingerCtx)
		}
	}

	// Schedule periodic SNMP discovery based on config interval
	discoveryTicker := time.NewTicker(cfg.DiscoveryInterval)
	defer discoveryTicker.Stop()

	log.Printf("Starting discovery loop (interval: %v)...", cfg.DiscoveryInterval)
	for {
		select {
		case <-ctx.Done():
			log.Println("Shutdown signal received. Stopping pingers and exiting.")
			for _, cancel := range activePingers {
				cancel()
			}
			return
		case <-discoveryTicker.C:
			log.Println("Running full discovery scan...")
			devices := discovery.RunFullDiscovery(cfg)
			log.Printf("Discovery found %d devices.", len(devices))
			for _, dev := range devices {
				log.Printf("Device found: %s (%s)", dev.IP, dev.Hostname)
				mgr.Add(dev)
				mgr.UpdateLastSeen(dev.IP)
				// Write device info to InfluxDB (only when discovered/changed)
				if err := writer.WriteDeviceInfo(dev.IP, dev.Hostname, dev.Hostname, dev.SysDescr, dev.SysObjectID); err != nil {
					log.Printf("Failed to write device info for %s: %v", dev.IP, err)
				}
				if _, running := activePingers[dev.IP]; !running {
					log.Printf("Starting pinger for %s (%s)", dev.IP, dev.Hostname)
					pingerCtx, cancel := context.WithCancel(ctx)
					activePingers[dev.IP] = cancel
					go monitoring.StartPinger(dev, cfg, writer, pingerCtx)
				}
			}
			// Remove devices not seen in last 2 discovery cycles (interval * 2)
			pruned := mgr.Prune(cfg.DiscoveryInterval * 2)
			for _, old := range pruned {
				log.Printf("Pruning device: %s (%s)", old.IP, old.Hostname)
				if cancel, ok := activePingers[old.IP]; ok {
					cancel()
					delete(activePingers, old.IP)
				}
			}
		}
	}
}
