package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"runtime"
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

	// Validate configuration for security and sanity
	warning, err := config.ValidateConfig(cfg)
	if err != nil {
		log.Fatalf("invalid configuration: %v", err)
	}
	if warning != "" {
		log.Printf("Configuration warning: %s", warning)
	}

	mgr := state.NewManager(cfg.MaxDevices)
	writer := influx.NewWriter(cfg.InfluxDB.URL, cfg.InfluxDB.Token, cfg.InfluxDB.Org, cfg.InfluxDB.Bucket)
	defer writer.Close()

	// Map IP addresses to their pinger cancellation functions for cleanup
	activePingers := make(map[string]context.CancelFunc)

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	// Helper function to start a pinger with resource limits
	startPinger := func(dev state.Device) {
		if len(activePingers) >= cfg.MaxConcurrentPingers {
			log.Printf("Maximum concurrent pingers (%d) reached, skipping %s", cfg.MaxConcurrentPingers, dev.IP)
			return
		}
		if _, running := activePingers[dev.IP]; !running {
			log.Printf("Starting pinger for %s", dev.IP)
			pingerCtx, cancel := context.WithCancel(ctx)
			activePingers[dev.IP] = cancel
			go monitoring.StartPinger(dev, cfg, writer, pingerCtx)
		}
	}

	// Memory monitoring function
	checkMemoryUsage := func() {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		memoryMB := m.Alloc / 1024 / 1024
		if memoryMB > uint64(cfg.MemoryLimitMB) {
			log.Printf("WARNING: Memory usage (%d MB) exceeds limit (%d MB)", memoryMB, cfg.MemoryLimitMB)
		}
	}

	go func() {
		<-sigChan
		log.Println("Shutdown signal received. Stopping pingers and exiting.")
		stop()
	}()

	if *icmpOnly {
		log.Printf("--icmp-only flag set: Running ping-based discovery every %v, pinging online devices every %v...", cfg.IcmpDiscoveryInterval, cfg.PingInterval)
		// Execute initial ICMP discovery scan at startup across all networks
		log.Println("Running initial ping discovery scan...")
		var allDevices []state.Device
		for _, network := range cfg.Networks {
			log.Printf("Scanning network: %s", network)
			devices := discovery.RunPingDiscovery(network, cfg.IcmpWorkers)
			allDevices = append(allDevices, devices...)
			log.Printf("Network %s found %d online devices.", network, len(devices))
		}
		log.Printf("Initial ping discovery found %d online devices across all networks.", len(allDevices))
		for _, dev := range allDevices {
			log.Printf("Online device: %s", dev.IP)
			mgr.Add(dev)
			mgr.UpdateLastSeen(dev.IP)
			startPinger(dev)
		}

		// Schedule periodic ICMP discovery using configured interval
		discoveryTicker := time.NewTicker(cfg.IcmpDiscoveryInterval)
		defer discoveryTicker.Stop()
		lastScanTime := time.Now().Add(-cfg.MinScanInterval) // Allow immediate first scan

		for {
			select {
			case <-ctx.Done():
				log.Println("Shutdown signal received. Stopping pingers and exiting.")
				for _, cancel := range activePingers {
					cancel()
				}
				return
			case <-discoveryTicker.C:
				// Rate limiting check
				if time.Since(lastScanTime) < cfg.MinScanInterval {
					log.Printf("ICMP discovery scan rate limited (min interval: %v), skipping", cfg.MinScanInterval)
					continue
				}
				lastScanTime = time.Now()

				// Memory usage check
				checkMemoryUsage()

				log.Println("Running ping discovery scan...")
				var allDevices []state.Device
				for _, network := range cfg.Networks {
					log.Printf("Scanning network: %s", network)
					devices := discovery.RunPingDiscovery(network, cfg.IcmpWorkers)
					allDevices = append(allDevices, devices...)
				}
				log.Printf("Ping discovery found %d online devices across all networks.", len(allDevices))
				for _, dev := range allDevices {
					log.Printf("Online device: %s", dev.IP)
					mgr.Add(dev)
					mgr.UpdateLastSeen(dev.IP)
					startPinger(dev)
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
			startPinger(dev)
		}
	}

	// Schedule periodic SNMP discovery based on config interval
	discoveryTicker := time.NewTicker(cfg.DiscoveryInterval)
	defer discoveryTicker.Stop()

	log.Printf("Starting discovery loop (interval: %v)...", cfg.DiscoveryInterval)
	lastScanTime := time.Now().Add(-cfg.MinScanInterval) // Allow immediate first scan

	for {
		select {
		case <-ctx.Done():
			log.Println("Shutdown signal received. Stopping pingers and exiting.")
			for _, cancel := range activePingers {
				cancel()
			}
			return
		case <-discoveryTicker.C:
			// Rate limiting check
			if time.Since(lastScanTime) < cfg.MinScanInterval {
				log.Printf("Discovery scan rate limited (min interval: %v), skipping", cfg.MinScanInterval)
				continue
			}
			lastScanTime = time.Now()

			// Memory usage check
			checkMemoryUsage()

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
					startPinger(dev)
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
