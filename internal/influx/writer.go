package influx

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
	"github.com/influxdata/influxdb-client-go/v2/api/write"
	"github.com/rs/zerolog/log"
)

// Writer handles InfluxDB v2 time-series data writes with batching
type Writer struct {
	client   influxdb2.Client // InfluxDB client instance
	writeAPI api.WriteAPI     // Non-blocking write API for batching
	org      string           // InfluxDB organization name
	bucket   string           // InfluxDB bucket name

	// Batching fields - using channel for lock-free operation
	batchChan   chan *write.Point
	batchSize   int
	flushTicker *time.Ticker
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewWriter creates a new InfluxDB writer with batching support
func NewWriter(url, token, org, bucket string, batchSize int, flushInterval time.Duration) *Writer {
	client := influxdb2.NewClient(url, token)
	writeAPI := client.WriteAPI(org, bucket)

	ctx, cancel := context.WithCancel(context.Background())

	w := &Writer{
		client:      client,
		writeAPI:    writeAPI,
		org:         org,
		bucket:      bucket,
		batchChan:   make(chan *write.Point, batchSize*2), // Buffered channel for lock-free writes
		batchSize:   batchSize,
		flushTicker: time.NewTicker(flushInterval),
		ctx:         ctx,
		cancel:      cancel,
	}

	// Start background flusher
	go w.backgroundFlusher()

	return w
}

// backgroundFlusher periodically flushes batched points
func (w *Writer) backgroundFlusher() {
	// Panic recovery for background goroutine
	defer func() {
		if r := recover(); r != nil {
			log.Error().
				Interface("panic", r).
				Msg("Background flusher panic recovered")
		}
	}()

	// Start error monitoring goroutine
	go w.monitorWriteErrors()

	// Local batch for accumulating points
	batch := make([]*write.Point, 0, w.batchSize)

	for {
		select {
		case <-w.ctx.Done():
			// Drain remaining points from channel before shutdown
			w.drainAndFlush(batch)
			return

		case <-w.flushTicker.C:
			// Time-based flush
			if len(batch) > 0 {
				w.flushBatch(batch)
				batch = make([]*write.Point, 0, w.batchSize)
			}

		case point := <-w.batchChan:
			// Accumulate point
			batch = append(batch, point)
			
			// Flush when batch is full
			if len(batch) >= w.batchSize {
				w.flushBatch(batch)
				batch = make([]*write.Point, 0, w.batchSize)
			}
		}
	}
}

// drainAndFlush drains all remaining points from channel and flushes them
func (w *Writer) drainAndFlush(currentBatch []*write.Point) {
	// Collect any remaining points in current batch
	batch := currentBatch

	// Drain the channel
	for {
		select {
		case point := <-w.batchChan:
			batch = append(batch, point)
		default:
			// Channel is empty
			if len(batch) > 0 {
				w.flushBatch(batch)
			}
			return
		}
	}
}

// monitorWriteErrors monitors the write API error channel and logs errors
func (w *Writer) monitorWriteErrors() {
	// Panic recovery for error monitor goroutine
	defer func() {
		if r := recover(); r != nil {
			log.Error().
				Interface("panic", r).
				Msg("Write error monitor panic recovered")
		}
	}()

	errorChan := w.writeAPI.Errors()
	for {
		select {
		case <-w.ctx.Done():
			return
		case err := <-errorChan:
			if err != nil {
				log.Error().
					Err(err).
					Msg("InfluxDB write error detected")
			}
		}
	}
}

// HealthCheck verifies InfluxDB connectivity
func (w *Writer) HealthCheck() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Use the health check API
	health, err := w.client.Health(ctx)
	if err != nil {
		return fmt.Errorf("influxdb health check failed: %v", err)
	}

	if health.Status != "pass" {
		return fmt.Errorf("influxdb health status: %s", health.Status)
	}

	return nil
}

// WriteDeviceInfo writes device metadata to InfluxDB (call once per device or when SNMP data changes)
func (w *Writer) WriteDeviceInfo(ip, hostname, sysDescr string) error {
	// Validate IP address
	if err := validateIPAddress(ip); err != nil {
		return fmt.Errorf("invalid IP address for device info: %v", err)
	}

	// Sanitize string fields to prevent injection or corruption
	hostname = sanitizeInfluxString(hostname, "hostname")
	sysDescr = sanitizeInfluxString(sysDescr, "sysDescr")

	p := influxdb2.NewPoint(
		"device_info",
		map[string]string{"ip": ip},
		map[string]interface{}{
			"hostname":         hostname,
			"snmp_description": sysDescr,
		},
		time.Now(),
	)

	w.addToBatch(p)
	return nil
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

	p := influxdb2.NewPoint(
		"ping",
		map[string]string{"ip": ip},
		map[string]interface{}{
			"rtt_ms":  float64(rtt.Nanoseconds()) / 1e6,
			"success": successful,
		},
		time.Now(),
	)

	w.addToBatch(p)
	return nil
}

// addToBatch adds a point to the batch channel (lock-free operation)
func (w *Writer) addToBatch(point *write.Point) {
	select {
	case w.batchChan <- point:
		// Point added successfully
	case <-w.ctx.Done():
		// Context cancelled, drop point
	default:
		// Channel full, log warning but don't block
		log.Warn().Msg("Batch channel full, dropping point to avoid blocking")
	}
}

// flushBatch writes a batch of points to InfluxDB with retry logic
func (w *Writer) flushBatch(points []*write.Point) {
	if len(points) == 0 {
		return
	}

	// Write batch to InfluxDB with retry on failure
	w.flushWithRetry(points, 3)
}

// flushWithRetry attempts to write points with exponential backoff retry
func (w *Writer) flushWithRetry(points []*write.Point, maxRetries int) {
	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Write all points in the batch
		for _, point := range points {
			w.writeAPI.WritePoint(point)
		}

		// Force a flush to check for immediate errors
		w.writeAPI.Flush()

		// Wait a short time to see if errors appear
		time.Sleep(100 * time.Millisecond)

		// Check error channel with timeout
		select {
		case err := <-w.writeAPI.Errors():
			if err != nil {
				if attempt < maxRetries {
					backoffDuration := time.Duration(1<<uint(attempt)) * time.Second
					log.Warn().
						Err(err).
						Int("attempt", attempt+1).
						Int("max_retries", maxRetries).
						Dur("backoff", backoffDuration).
						Msg("InfluxDB write failed, retrying with backoff")
					time.Sleep(backoffDuration)
					continue
				} else {
					log.Error().
						Err(err).
						Int("points", len(points)).
						Msg("InfluxDB write failed after all retries")
					return
				}
			}
		default:
			// No error, write successful
			log.Info().
				Int("points", len(points)).
				Msg("Successfully flushed points to InfluxDB")
			return
		}
	}
}

// Close terminates the InfluxDB client connection
func (w *Writer) Close() {
	w.cancel()           // Stop background flusher (which will drain remaining points)
	w.flushTicker.Stop() // Stop flush ticker
	time.Sleep(100 * time.Millisecond) // Give background flusher time to finish
	w.writeAPI.Flush()   // Flush write API buffer
	w.client.Close()
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
