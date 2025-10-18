package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/extkljajicm/netscan/internal/config"
	"github.com/extkljajicm/netscan/internal/discovery"
	"github.com/extkljajicm/netscan/internal/influx"
	"github.com/extkljajicm/netscan/internal/monitoring"
	"github.com/extkljajicm/netscan/internal/state"
)

func main() {
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

	// Initialize state manager (single source of truth for devices)
	stateMgr := state.NewManager(cfg.MaxDevices)

	// Initialize InfluxDB writer with health check
	writer := influx.NewWriter(cfg.InfluxDB.URL, cfg.InfluxDB.Token, cfg.InfluxDB.Org, cfg.InfluxDB.Bucket)
	defer writer.Close()

	log.Println("Checking InfluxDB connectivity...")
	if err := writer.HealthCheck(); err != nil {
		log.Fatalf("InfluxDB connection failed: %v", err)
	}
	log.Println("InfluxDB connection successful ✓")

	// Map IP addresses to their pinger cancellation functions
	// CRITICAL: Protected by mutex to prevent concurrent map access
	activePingers := make(map[string]context.CancelFunc)
	var pingersMu sync.Mutex

	// WaitGroup for tracking all pinger goroutines
	var pingerWg sync.WaitGroup

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	mainCtx, stop := context.WithCancel(context.Background())
	defer stop()

	// Memory monitoring function
	checkMemoryUsage := func() {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		memoryMB := m.Alloc / 1024 / 1024
		if memoryMB > uint64(cfg.MemoryLimitMB) {
			log.Printf("WARNING: Memory usage (%d MB) exceeds limit (%d MB)", memoryMB, cfg.MemoryLimitMB)
		}
	}

	// Ticker 1: ICMP Discovery Loop - finds new devices
	icmpDiscoveryTicker := time.NewTicker(cfg.IcmpDiscoveryInterval)
	defer icmpDiscoveryTicker.Stop()

	// Ticker 2: Daily SNMP Scan Loop - scheduled full SNMP scan
	var dailySNMPChan <-chan time.Time
	if cfg.SNMPDailySchedule != "" {
		dailySNMPChan = createDailySNMPChannel(cfg.SNMPDailySchedule)
	} else {
		// Create a dummy channel that never fires
		dailySNMPChan = make(<-chan time.Time)
	}

	// Ticker 3: Pinger Reconciliation Loop - ensures all devices have pingers
	reconciliationTicker := time.NewTicker(5 * time.Second)
	defer reconciliationTicker.Stop()

	// Ticker 4: State Pruning Loop - removes stale devices
	pruningTicker := time.NewTicker(1 * time.Hour)
	defer pruningTicker.Stop()

	// Run initial ICMP discovery at startup
	log.Println("Starting ICMP discovery scan...")
	responsiveIPs := discovery.RunICMPSweep(cfg.Networks, cfg.IcmpWorkers)
	log.Printf("ICMP discovery found %d online devices", len(responsiveIPs))
	
	for _, ip := range responsiveIPs {
		isNew := stateMgr.AddDevice(ip)
		if isNew {
			log.Printf("New device found: %s. Performing initial SNMP scan.", ip)
			// Trigger immediate SNMP scan in background
			go func(newIP string) {
				snmpDevices := discovery.RunSNMPScan([]string{newIP}, &cfg.SNMP, cfg.SnmpWorkers)
				if len(snmpDevices) > 0 {
					dev := snmpDevices[0]
					stateMgr.UpdateDeviceSNMP(dev.IP, dev.Hostname, dev.SysDescr, dev.SysObjectID)
					// Write device info to InfluxDB
					if err := writer.WriteDeviceInfo(dev.IP, dev.Hostname, dev.Hostname, dev.SysDescr, dev.SysObjectID); err != nil {
						log.Printf("Failed to write device info for %s: %v", dev.IP, err)
					}
					log.Printf("Device enriched: %s (%s)", dev.IP, dev.Hostname)
				}
			}(ip)
		}
	}

	// Shutdown handler
	go func() {
		<-sigChan
		log.Println("Shutdown signal received. Stopping all operations...")
		stop()
	}()

	log.Printf("Starting monitoring loops...")
	log.Printf("- ICMP Discovery: every %v", cfg.IcmpDiscoveryInterval)
	if cfg.SNMPDailySchedule != "" {
		log.Printf("- Daily SNMP Scan: at %s", cfg.SNMPDailySchedule)
	}
	log.Printf("- Pinger Reconciliation: every 5s")
	log.Printf("- State Pruning: every 1h")

	// Main event loop with all tickers
	for {
		select {
		case <-mainCtx.Done():
			// Graceful shutdown
			log.Println("Shutting down all pingers...")
			
			// Stop all tickers
			icmpDiscoveryTicker.Stop()
			reconciliationTicker.Stop()
			pruningTicker.Stop()
			
			// Cancel all active pingers
			pingersMu.Lock()
			for ip, cancel := range activePingers {
				log.Printf("Stopping pinger for %s", ip)
				cancel()
			}
			pingersMu.Unlock()
			
			// Wait for all pingers to exit
			log.Println("Waiting for all pingers to stop...")
			pingerWg.Wait()
			
			log.Println("Shutdown complete.")
			return

		case <-icmpDiscoveryTicker.C:
			// ICMP Discovery: Find new devices
			checkMemoryUsage()
			log.Println("Starting ICMP discovery scan...")
			responsiveIPs := discovery.RunICMPSweep(cfg.Networks, cfg.IcmpWorkers)
			log.Printf("ICMP discovery found %d online devices", len(responsiveIPs))
			
			for _, ip := range responsiveIPs {
				isNew := stateMgr.AddDevice(ip)
				if isNew {
					log.Printf("New device found: %s. Performing initial SNMP scan.", ip)
					// Trigger immediate SNMP scan in background
					go func(newIP string) {
						snmpDevices := discovery.RunSNMPScan([]string{newIP}, &cfg.SNMP, cfg.SnmpWorkers)
						if len(snmpDevices) > 0 {
							dev := snmpDevices[0]
							stateMgr.UpdateDeviceSNMP(dev.IP, dev.Hostname, dev.SysDescr, dev.SysObjectID)
							// Write device info to InfluxDB
							if err := writer.WriteDeviceInfo(dev.IP, dev.Hostname, dev.Hostname, dev.SysDescr, dev.SysObjectID); err != nil {
								log.Printf("Failed to write device info for %s: %v", dev.IP, err)
							}
							log.Printf("Device enriched: %s (%s)", dev.IP, dev.Hostname)
						}
					}(ip)
				}
			}

		case <-dailySNMPChan:
			// Daily SNMP Scan: Full scan of all known devices
			log.Println("Starting daily full SNMP scan...")
			allIPs := stateMgr.GetAllIPs()
			log.Printf("Performing SNMP scan on %d devices...", len(allIPs))
			snmpDevices := discovery.RunSNMPScan(allIPs, &cfg.SNMP, cfg.SnmpWorkers)
			log.Printf("SNMP scan complete, enriched %d devices", len(snmpDevices))
			
			for _, dev := range snmpDevices {
				stateMgr.UpdateDeviceSNMP(dev.IP, dev.Hostname, dev.SysDescr, dev.SysObjectID)
				// Write device info to InfluxDB
				if err := writer.WriteDeviceInfo(dev.IP, dev.Hostname, dev.Hostname, dev.SysDescr, dev.SysObjectID); err != nil {
					log.Printf("Failed to write device info for %s: %v", dev.IP, err)
				}
			}
			log.Println("Daily SNMP scan complete.")

		case <-reconciliationTicker.C:
			// Pinger Reconciliation: Ensure all devices have pingers
			pingersMu.Lock()
			
			// Get current state IPs
			currentIPs := stateMgr.GetAllIPs()
			currentIPMap := make(map[string]bool)
			for _, ip := range currentIPs {
				currentIPMap[ip] = true
			}
			
			// Start pingers for new devices
			for ip := range currentIPMap {
				if _, exists := activePingers[ip]; !exists {
					if len(activePingers) >= cfg.MaxConcurrentPingers {
						log.Printf("Maximum concurrent pingers (%d) reached, skipping %s", cfg.MaxConcurrentPingers, ip)
						continue
					}
					log.Printf("Starting continuous pinger for %s", ip)
					pingerCtx, pingerCancel := context.WithCancel(mainCtx)
					activePingers[ip] = pingerCancel
					
					// Get device info for logging
					dev, exists := stateMgr.Get(ip)
					if !exists {
						dev = &state.Device{IP: ip, Hostname: ip}
					}
					
					pingerWg.Add(1)
					go monitoring.StartPinger(pingerCtx, &pingerWg, *dev, cfg.PingInterval, writer, stateMgr)
				}
			}
			
			// Stop pingers for removed devices
			for ip, cancelFunc := range activePingers {
				if !currentIPMap[ip] {
					log.Printf("Stopping continuous pinger for stale device %s", ip)
					cancelFunc()
					delete(activePingers, ip)
				}
			}
			
			pingersMu.Unlock()

		case <-pruningTicker.C:
			// State Pruning: Remove devices not seen recently
			log.Println("Pruning stale devices...")
			pruned := stateMgr.PruneStale(24 * time.Hour)
			if len(pruned) > 0 {
				log.Printf("Pruned %d stale devices", len(pruned))
				for _, dev := range pruned {
					log.Printf("Pruned device: %s (%s)", dev.IP, dev.Hostname)
				}
			}
		}
	}
}

// createDailySNMPChannel creates a channel that fires at the specified time each day
func createDailySNMPChannel(timeStr string) <-chan time.Time {
	// Parse the time (HH:MM format)
	var hour, minute int
	t, err := time.Parse("15:04", timeStr)
	if err != nil {
		log.Printf("Warning: Invalid SNMP daily schedule format %s, using default 02:00", timeStr)
		hour, minute = 2, 0
	} else {
		hour = t.Hour()
		minute = t.Minute()
	}

	ch := make(chan time.Time, 1)

	go func() {
		for {
			// Calculate duration until next scheduled time
			now := time.Now()
			nextRun := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
			if nextRun.Before(now) {
				nextRun = nextRun.Add(24 * time.Hour)
			}
			durationUntilNext := time.Until(nextRun)

			log.Printf("Next daily SNMP scan scheduled at %s (in %v)", nextRun.Format("2006-01-02 15:04:05"), durationUntilNext)

			// Wait until the scheduled time
			time.Sleep(durationUntilNext)

			// Send the tick
			ch <- time.Now()
		}
	}()

	return ch
}
