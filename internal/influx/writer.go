package influx

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
)

// Writer handles InfluxDB v2 time-series data writes
type Writer struct {
	client      influxdb2.Client     // InfluxDB client instance
	writeAPI    api.WriteAPIBlocking // Blocking write API for synchronous writes
	org         string               // InfluxDB organization name
	bucket      string               // InfluxDB bucket name
	rateLimiter *time.Ticker         // Rate limiter for write operations
	lastWrite   time.Time            // Last write timestamp for rate limiting
	mu          sync.Mutex           // Protects rate limiting state
}

// NewWriter creates a new InfluxDB writer with blocking write API
func NewWriter(url, token, org, bucket string) *Writer {
	client := influxdb2.NewClient(url, token)
	writeAPI := client.WriteAPIBlocking(org, bucket)
	return &Writer{
		client:      client,
		writeAPI:    writeAPI,
		org:         org,
		bucket:      bucket,
		rateLimiter: time.NewTicker(10 * time.Millisecond), // Allow ~100 writes/second
		lastWrite:   time.Now(),
	}
}

// WriteDeviceInfo writes device metadata to InfluxDB (call once per device or when SNMP data changes)
func (w *Writer) WriteDeviceInfo(ip, hostname, sysName, sysDescr, sysObjectID string) error {
	// Validate IP address
	if err := validateIPAddress(ip); err != nil {
		return fmt.Errorf("invalid IP address for device info: %v", err)
	}

	// Sanitize string fields to prevent injection or corruption
	hostname = sanitizeInfluxString(hostname, "hostname")
	sysName = sanitizeInfluxString(sysName, "sysName")
	sysDescr = sanitizeInfluxString(sysDescr, "sysDescr")
	sysObjectID = sanitizeInfluxString(sysObjectID, "sysObjectID")

	// Rate limiting
	w.rateLimit()

	p := influxdb2.NewPointWithMeasurement("device_info")
	p.AddTag("ip", ip) // Stable identifier
	p.AddField("hostname", hostname)
	p.AddField("snmp_name", sysName)
	p.AddField("snmp_description", sysDescr)
	p.AddField("snmp_sysid", sysObjectID)
	p.SetTime(time.Now())
	return w.writeAPI.WritePoint(context.Background(), p)
}

// WritePingResult writes ICMP ping metrics to InfluxDB (optimized for time-series)
func (w *Writer) WritePingResult(ip string, rtt time.Duration, successful bool) error {
	// Validate IP address
	if err := validateIPAddress(ip); err != nil {
		return fmt.Errorf("invalid IP address for ping result: %v", err)
	}

	// Validate RTT values
	if rtt < 0 {
		return fmt.Errorf("invalid RTT value: %v (cannot be negative)", rtt)
	}
	if rtt > time.Minute {
		return fmt.Errorf("invalid RTT value: %v (too high, max 1 minute)", rtt)
	}

	// Rate limiting
	w.rateLimit()

	p := influxdb2.NewPointWithMeasurement("ping")
	p.AddTag("ip", ip)                                   // Only IP as tag for low cardinality
	p.AddField("rtt_ms", float64(rtt.Nanoseconds())/1e6) // Convert to float milliseconds
	p.AddField("success", successful)
	p.SetTime(time.Now())
	return w.writeAPI.WritePoint(context.Background(), p)
}

// Close terminates the InfluxDB client connection
func (w *Writer) Close() {
	w.rateLimiter.Stop()
	w.client.Close()
}

// rateLimit enforces write rate limiting
func (w *Writer) rateLimit() {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Simple rate limiting: ensure minimum 10ms between writes
	elapsed := time.Since(w.lastWrite)
	if elapsed < 10*time.Millisecond {
		time.Sleep(10*time.Millisecond - elapsed)
	}
	w.lastWrite = time.Now()
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

	// Security checks - prevent writing data for dangerous addresses
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

	return nil
}

// sanitizeInfluxString sanitizes strings for safe InfluxDB storage
func sanitizeInfluxString(s, fieldName string) string {
	if s == "" {
		return ""
	}

	originalLen := len(s)

	// Limit string length to prevent database issues
	if len(s) > 500 {
		s = s[:500] + "..."
	}

	// Remove or replace characters that could cause issues in InfluxDB
	// InfluxDB field values can contain most characters, but we'll be conservative
	s = strings.Map(func(r rune) rune {
		// Remove control characters except tab and newline
		if r < 32 && r != 9 && r != 10 {
			return -1
		}
		// Allow most printable characters
		return r
	}, s)

	// Trim whitespace
	result := strings.TrimSpace(s)

	// Log if string was significantly modified (for debugging)
	if len(result) != originalLen {
		// Could add logging here if needed, but for now just use fieldName to avoid unused parameter warning
		_ = fieldName
	}

	return result
}
