package discovery

import (
	"net"
	"sync"
	"time"

	"github.com/gosnmp/gosnmp"
	"github.com/marko/netscan/internal/config"
	"github.com/marko/netscan/internal/state"
)

func RunScan(cfg *config.Config) []state.Device {
	var (
		jobs    = make(chan string, 256)
		results = make(chan state.Device, 256)
		wg      sync.WaitGroup
	)

	worker := func() {
		defer wg.Done()
		for ip := range jobs {
			params := &gosnmp.GoSNMP{
				Target:    ip,
				Port:      uint16(cfg.SNMP.Port),
				Community: cfg.SNMP.Community,
				Version:   gosnmp.Version2c,
				Timeout:   cfg.SNMP.Timeout,
				Retries:   cfg.SNMP.Retries,
			}
			if err := params.Connect(); err != nil {
				continue
			}
			defer params.Conn.Close()
			oids := []string{"1.3.6.1.2.1.1.5.0", "1.3.6.1.2.1.1.1.0", "1.3.6.1.2.1.1.2.0"}
			resp, err := params.Get(oids)
			if err != nil || len(resp.Variables) < 3 {
				continue
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

	// Start workers
	workerCount := 64
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go worker()
	}

	// Producer: generate IPs from CIDR
	for _, cidr := range cfg.Networks {
		ips := ipsFromCIDR(cidr)
		for _, ip := range ips {
			jobs <- ip
		}
	}
	close(jobs)

	go func() {
		wg.Wait()
		close(results)
	}()

	var found []state.Device
	for dev := range results {
		found = append(found, dev)
	}
	return found
}

func ipsFromCIDR(cidr string) []string {
	var ips []string
	ip, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return ips
	}
	for ip := ip.Mask(ipnet.Mask); ipnet.Contains(ip); incIP(ip) {
		ips = append(ips, ip.String())
	}
	return ips
}

func incIP(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}
