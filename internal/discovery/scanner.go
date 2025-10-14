package discovery

import (
	"log"
	"net"
	"sync"
	"time"

	"github.com/extkljajicm/netscan/internal/config"
	"github.com/extkljajicm/netscan/internal/state"
	"github.com/gosnmp/gosnmp"
	probing "github.com/prometheus-community/pro-bing"
)

// RunScanIPsOnly returns all IP addresses in the specified CIDR range
func RunScanIPsOnly(cidr string) []string {
	return ipsFromCIDR(cidr)
}

// RunScan performs concurrent SNMPv2c discovery across configured networks
func RunScan(cfg *config.Config) []state.Device {
	var (
		jobs    = make(chan string, 256)       // Buffered channel for IP addresses to scan
		results = make(chan state.Device, 256) // Buffered channel for discovered devices
		wg      sync.WaitGroup
	)

	// Worker goroutine for SNMP queries
	worker := func() {
		defer wg.Done()
		for ip := range jobs {
			// Configure SNMP connection parameters
			params := &gosnmp.GoSNMP{
				Target:    ip,
				Port:      uint16(cfg.SNMP.Port),
				Community: cfg.SNMP.Community,
				Version:   gosnmp.Version2c,
				Timeout:   cfg.SNMP.Timeout,
				Retries:   cfg.SNMP.Retries,
			}
			if err := params.Connect(); err != nil {
				continue // Skip unresponsive devices
			}
			// Query standard MIB-II system OIDs: sysName, sysDescr, sysObjectID
			oids := []string{"1.3.6.1.2.1.1.5.0", "1.3.6.1.2.1.1.1.0", "1.3.6.1.2.1.1.2.0"}
			resp, err := params.Get(oids)
			params.Conn.Close()
			if err != nil || len(resp.Variables) < 3 {
				continue // Skip devices with incomplete SNMP responses
			}
			dev := state.Device{
				IP:          ip,
				Hostname:    resp.Variables[0].Value.(string),
				SysDescr:    resp.Variables[1].Value.(string),
				SysObjectID: resp.Variables[2].Value.(string),
				LastSeen:    time.Now(),
			}
			results <- dev
		}
	}

	// Launch concurrent SNMP worker goroutines
	workerCount := cfg.SnmpWorkers
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go worker()
	}

	// Producer: enqueue all IPs from configured CIDR ranges
	for _, cidr := range cfg.Networks {
		ips := ipsFromCIDR(cidr)
		for _, ip := range ips {
			jobs <- ip
		}
	}
	close(jobs)

	// Wait for all workers to complete, then close results channel
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect all discovered devices
	var found []state.Device
	for dev := range results {
		found = append(found, dev)
	}
	return found
}

// RunPingDiscovery performs concurrent ICMP ping sweep to find online devices
func RunPingDiscovery(cidr string, icmpWorkers int) []state.Device {
	var (
		jobs    = make(chan string, 256)       // Buffered channel for IP addresses to ping
		results = make(chan state.Device, 256) // Buffered channel for responsive devices
		wg      sync.WaitGroup
	)

	// Worker goroutine for ICMP ping probes
	worker := func() {
		defer wg.Done()
		for ip := range jobs {
			pinger, err := probing.NewPinger(ip)
			if err != nil {
				continue // Skip invalid IP addresses
			}
			pinger.Count = 1                 // Single ping per device
			pinger.Timeout = 1 * time.Second // 1-second discovery timeout
			pinger.SetPrivileged(true)       // Use raw sockets for ICMP
			if err := pinger.Run(); err != nil {
				continue // Skip ping failures
			}
			stats := pinger.Statistics()
			if stats.PacketsRecv > 0 { // Device responded to ping
				results <- state.Device{
					IP:       ip,
					Hostname: ip, // Use IP as hostname for ping-discovered devices
					LastSeen: time.Now(),
				}
			}
		}
	}

	// Launch concurrent ping worker goroutines
	workerCount := icmpWorkers
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go worker()
	}

	// Producer: enqueue all IPs from CIDR range
	ips := ipsFromCIDR(cidr)
	for _, ip := range ips {
		jobs <- ip
	}
	close(jobs)

	// Wait for all workers to complete, then close results channel
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect all ping-responsive devices
	var devices []state.Device
	for dev := range results {
		devices = append(devices, dev)
	}
	return devices
}

// RunFullDiscovery performs ICMP ping sweep first, then SNMP polling of online devices
func RunFullDiscovery(cfg *config.Config) []state.Device {
	var (
		jobs    = make(chan string, 256)       // Buffered channel for IP addresses to scan
		results = make(chan state.Device, 256) // Buffered channel for discovered devices
		wg      sync.WaitGroup
	)

	// Worker goroutine for SNMP queries (only on ping-responsive devices)
	worker := func() {
		defer wg.Done()
		for ip := range jobs {
			// Configure SNMP connection parameters
			params := &gosnmp.GoSNMP{
				Target:    ip,
				Port:      uint16(cfg.SNMP.Port),
				Community: cfg.SNMP.Community,
				Version:   gosnmp.Version2c,
				Timeout:   cfg.SNMP.Timeout,
				Retries:   cfg.SNMP.Retries,
			}
			if err := params.Connect(); err != nil {
				// SNMP failed, but device is online (from ICMP), so add basic device info
				results <- state.Device{
					IP:          ip,
					Hostname:    ip, // Use IP as hostname for non-SNMP devices
					SysDescr:    "ICMP-responsive device (SNMP unavailable)",
					SysObjectID: "",
					LastSeen:    time.Now(),
				}
				continue
			}
			// Query standard MIB-II system OIDs: sysName, sysDescr, sysObjectID
			oids := []string{"1.3.6.1.2.1.1.5.0", "1.3.6.1.2.1.1.1.0", "1.3.6.1.2.1.1.2.0"}
			resp, err := params.Get(oids)
			params.Conn.Close()
			if err != nil || len(resp.Variables) < 3 {
				// SNMP query failed, but device is online
				results <- state.Device{
					IP:          ip,
					Hostname:    ip,
					SysDescr:    "ICMP-responsive device (SNMP query failed)",
					SysObjectID: "",
					LastSeen:    time.Now(),
				}
				continue
			}
			dev := state.Device{
				IP:          ip,
				Hostname:    string(resp.Variables[0].Value.([]byte)),
				SysDescr:    string(resp.Variables[1].Value.([]byte)),
				SysObjectID: resp.Variables[2].Value.(string),
				LastSeen:    time.Now(),
			}
			results <- dev
		}
	}

	// First, perform ICMP ping sweep to find online devices
	log.Println("Performing ICMP discovery to find online devices...")
	onlineIPs := make([]string, 0)

	var icmpWg sync.WaitGroup
	icmpJobs := make(chan string, 256)
	icmpResults := make(chan string, 256)

	// ICMP worker goroutine
	icmpWorker := func() {
		defer icmpWg.Done()
		for ip := range icmpJobs {
			pinger, err := probing.NewPinger(ip)
			if err != nil {
				continue
			}
			pinger.Count = 1
			pinger.Timeout = 1 * time.Second
			pinger.SetPrivileged(true)
			if err := pinger.Run(); err != nil {
				continue
			}
			stats := pinger.Statistics()
			if stats.PacketsRecv > 0 {
				icmpResults <- ip
			}
		}
	}

	// Launch ICMP workers
	icmpWorkerCount := cfg.IcmpWorkers
	for i := 0; i < icmpWorkerCount; i++ {
		icmpWg.Add(1)
		go icmpWorker()
	}

	// Producer: enqueue all IPs from all configured networks
	for _, network := range cfg.Networks {
		ips := ipsFromCIDR(network)
		for _, ip := range ips {
			icmpJobs <- ip
		}
	}
	close(icmpJobs)

	// Wait for ICMP discovery to complete
	go func() {
		icmpWg.Wait()
		close(icmpResults)
	}()

	// Collect online IPs
	for ip := range icmpResults {
		onlineIPs = append(onlineIPs, ip)
	}

	log.Printf("ICMP discovery found %d online devices, now polling with SNMP...", len(onlineIPs))

	// Now perform SNMP polling only on online devices
	// Launch SNMP worker goroutines
	snmpWorkerCount := cfg.SnmpWorkers // Configurable SNMP workers
	for i := 0; i < snmpWorkerCount; i++ {
		wg.Add(1)
		go worker()
	}

	// Producer: enqueue online IPs for SNMP polling
	for _, ip := range onlineIPs {
		jobs <- ip
	}
	close(jobs)

	// Wait for all SNMP workers to complete
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect all discovered devices
	var devices []state.Device
	for dev := range results {
		devices = append(devices, dev)
	}
	return devices
}

// ipsFromCIDR expands CIDR notation into individual IP addresses
func ipsFromCIDR(cidr string) []string {
	var ips []string
	ip, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return ips
	}
	// Iterate through all IPs in the subnet
	for ip := ip.Mask(ipnet.Mask); ipnet.Contains(ip); incIP(ip) {
		ips = append(ips, ip.String())
	}
	return ips
}

// incIP increments an IP address by 1 (handles carry-over for IPv4)
func incIP(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}
