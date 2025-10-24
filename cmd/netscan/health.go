package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"time"

	"github.com/kljama/netscan/internal/influx"
	"github.com/kljama/netscan/internal/state"
	"github.com/rs/zerolog/log"
)

// HealthServer provides HTTP health check endpoint
type HealthServer struct {
	stateMgr  *state.Manager
	writer    *influx.Writer
	startTime time.Time
	port      int
}

// HealthResponse represents the health check JSON response
type HealthResponse struct {
	Status        string    `json:"status"`         // "healthy", "degraded", "unhealthy"
	Version       string    `json:"version"`        // Version string
	Uptime        string    `json:"uptime"`         // Human readable uptime
	DeviceCount   int       `json:"device_count"`   // Number of monitored devices
	ActivePingers int       `json:"active_pingers"` // Number of active pinger goroutines
	InfluxDBOK    bool      `json:"influxdb_ok"`    // InfluxDB connectivity status
	Goroutines    int       `json:"goroutines"`     // Current goroutine count
	MemoryMB      uint64    `json:"memory_mb"`      // Current memory usage in MB
	Timestamp     time.Time `json:"timestamp"`      // Current timestamp
}

// NewHealthServer creates a new health check server
func NewHealthServer(port int, stateMgr *state.Manager, writer *influx.Writer) *HealthServer {
	return &HealthServer{
		stateMgr:  stateMgr,
		writer:    writer,
		startTime: time.Now(),
		port:      port,
	}
}

// Start begins serving health checks (non-blocking)
func (hs *HealthServer) Start() error {
	http.HandleFunc("/health", hs.healthHandler)
	http.HandleFunc("/health/ready", hs.readinessHandler)
	http.HandleFunc("/health/live", hs.livenessHandler)

	addr := fmt.Sprintf(":%d", hs.port)
	go func() {
		// Panic recovery for health server goroutine
		defer func() {
			if r := recover(); r != nil {
				log.Error().
					Interface("panic", r).
					Msg("Health server panic recovered")
			}
		}()

		if err := http.ListenAndServe(addr, nil); err != nil {
			log.Error().Err(err).Msg("Health server error")
		}
	}()

	log.Info().Str("address", addr).Msg("Health check endpoint started")
	return nil
}

// healthHandler provides detailed health information
func (hs *HealthServer) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Get memory stats
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// Determine overall status
	influxOK := hs.writer.HealthCheck() == nil
	status := "healthy"
	if !influxOK {
		status = "degraded"
	}

	response := HealthResponse{
		Status:        status,
		Version:       "1.0.0", // TODO: Get from build-time variable
		Uptime:        time.Since(hs.startTime).String(),
		DeviceCount:   hs.stateMgr.Count(),
		ActivePingers: runtime.NumGoroutine(), // Approximate
		InfluxDBOK:    influxOK,
		Goroutines:    runtime.NumGoroutine(),
		MemoryMB:      m.Alloc / 1024 / 1024,
		Timestamp:     time.Now(),
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// readinessHandler indicates if service is ready to accept traffic
func (hs *HealthServer) readinessHandler(w http.ResponseWriter, r *http.Request) {
	// Service is ready if InfluxDB is accessible
	if err := hs.writer.HealthCheck(); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("NOT READY: InfluxDB unavailable"))
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("READY"))
}

// livenessHandler indicates if service is alive
func (hs *HealthServer) livenessHandler(w http.ResponseWriter, r *http.Request) {
	// If we can respond, we're alive
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ALIVE"))
}
