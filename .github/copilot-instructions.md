# GitHub Copilot Instructions for Project: netscan

## Project Overview

`netscan` is a production-grade Go network monitoring service (Go 1.25+) with a decoupled, multi-ticker architecture. **Target platform: linux-amd64 only.** The service is designed for containerized deployment via Docker Compose (recommended) with native systemd deployment as an alternative option.

### Core Workflow

The service implements four independent, concurrent monitoring workflows:

1.  **Periodic ICMP Discovery:** Configurable interval (default: 5m) scanning of network ranges to find responsive devices via ICMP ping sweeps.
2.  **Continuous ICMP Monitoring:** Every discovered device gets a dedicated, long-running pinger goroutine performing high-frequency (default: 2s interval) ICMP pings to track real-time uptime.
3.  **Dual-Trigger SNMP Scanning:**
    * **Initial Scan:** Immediate SNMP query when a device is first discovered to enrich device metadata (hostname, sysDescr).
    * **Scheduled Scan:** Daily full SNMP scan of *all* devices at a configurable time (e.g., 02:00 AM) to refresh metadata.
4.  **Resilient Data Persistence:** All monitoring (ICMP) and discovery (SNMP) data written to InfluxDB v2 with:
    * Batch write system (default: 100 points per batch, 5s flush interval) for 99% reduction in database requests
    * Background flusher goroutine with graceful shutdown
    * Health checks on startup
    * Data sanitization and validation
    * Structured error logging with context

## Deployment Architecture

### Platform Constraint
**Target platform: linux-amd64 only.** All documentation, builds, and deployment instructions focus exclusively on 64-bit x86 Linux systems. Multi-architecture support (ARM, etc.) is deferred to future work.

### Docker Deployment (Primary - Recommended)
- **Recommended approach** for ease of deployment and consistency
- Multi-stage Dockerfile (Go 1.25-alpine builder, alpine:latest runtime) producing ~15MB final image
- Docker Compose orchestrates netscan + InfluxDB v2.7 stack
- **Critical Security Note:** Container **must run as root** (non-negotiable requirement for ICMP raw socket access in Linux containers, even with CAP_NET_RAW)
- Host networking mode (`network_mode: host`) for direct network access to scan targets
- Environment variable expansion via docker-compose.yml (uses `.env` file or inline defaults)
- HEALTHCHECK directive using `/health/live` endpoint
- Automated deployment validation via `docker-verify.sh` script in CI/CD
- Configuration: mount `config.yml` as read-only volume at `/app/config.yml`

### Native Deployment (Alternative - Maximum Security)
- For environments requiring non-root execution outside containers
- Uses dedicated `netscan` system user (created by `deploy.sh`) with `/bin/false` shell
- CAP_NET_RAW capability via `setcap cap_net_raw+ep` on binary (no root execution required)
- Systemd service unit with hardened security settings (PrivateTmp, ProtectSystem=strict, NoNewPrivileges)
- Secure credential management via `/opt/netscan/.env` file (mode 600)
- Deployment location: `/opt/netscan/` with binary, config, and .env file
- Automated deployment via `deploy.sh`, cleanup via `undeploy.sh`

---

## 🏛️ Guiding Principles for New Features

All new code and features must adhere to these principles:

* **Decoupled & Concurrent:** New services (e.g., a new scanner type, a data export) MUST be implemented as decoupled, concurrent goroutines. They should be orchestrated by a dedicated Ticker in `main.go` and must not block other services.
* **Centralized State:** The `StateManager` is the **single source of truth** for all device state. New features MUST interact with the `StateManager` via its thread-safe methods. Do not create separate device lists.
* **Resilience First:** All new code interacting with external services (networks, databases, APIs) MUST implement:
    1.  Aggressive `context.WithTimeout`
    2.  Robust error handling (log the error and continue; never `panic`)
    3.  Client-side rate limiting where appropriate.
* **Configurable & Backward-Compatible:** All new parameters MUST be added to `config.yml`, support environment variable overrides (using `os.ExpandEnv()`), and include sensible defaults to ensure existing `config.yml` files still work.
* **Testability:** New features must be testable. Use interfaces (like `PingWriter` or `StateManager`) to allow for easy mocking in unit tests.
* **Secure by Default:** All string data from external sources (SNMP, device responses) MUST be sanitized before being written to InfluxDB or logged.

---

## ⛔ Architectural Boundaries & Non-Goals

To keep the project focused, we explicitly **do not** do the following. Do not suggest code that:

* **Adds a Web UI:** `netscan` is a headless backend service. A UI is out of scope.
* **Adds New Databases:** Data persistence is **exclusively for InfluxDB**. Do not suggest adding support for Prometheus, MySQL, PostgreSQL, etc.
* **Performs Network Modification:** This is a *read-only* monitoring tool. It must never perform active network changes (e.g., blocking IPs, modifying device configs).
* **Bypasses the `StateManager`:** All device discovery and monitoring must be funneled through the central state.
* **Uses `root` for non-ICMP tasks:** The `root` user in Docker is *only* for ICMP raw sockets. All other operations should be possible as a non-root user (even if the container runs as root).

---

## Core Features (Currently Implemented)

### Configuration System
* **YAML Configuration:** `config.yml` loaded with full environment variable expansion via `os.ExpandEnv()`
* **CLI Flag Support:** `-config` flag to specify custom configuration file path (default: `config.yml`)
* **Environment Variable Expansion:** All `${VAR_NAME}` placeholders in config.yml automatically expanded
* **Backward Compatibility:** Optional `discovery_interval` field (deprecated, defaults to 4h if omitted) for legacy configs
* **Validation on Startup:** Comprehensive security and sanity checks with clear error messages

**Key Configuration Parameters:**
* `icmp_discovery_interval` (duration): How often to scan for new devices (e.g., "5m")
* `ping_interval` (duration): How often to ping known devices (e.g., "2s")
* `ping_timeout` (duration): Timeout for individual ping operations (e.g., "2s")
* `snmp_daily_schedule` (string): Time for daily SNMP scan in HH:MM format (e.g., "02:00"), empty string disables
* `health_check_port` (int): HTTP server port for health endpoints (default: 8080)
* `batch_size` (int): InfluxDB batch size for performance (default: 100)
* `flush_interval` (duration): InfluxDB batch flush interval (default: "5s")
* `icmp_workers` (int): Concurrent ICMP discovery workers (default: 64)
* `snmp_workers` (int): Concurrent SNMP scanning workers (default: 32)
* `max_concurrent_pingers` (int): Maximum number of active pinger goroutines (default: 1000)
* `max_devices` (int): Maximum devices to monitor with LRU eviction (default: 10000)
* `min_scan_interval` (duration): Minimum discovery scan interval for rate limiting (default: "1m")
* `memory_limit_mb` (int): Memory usage warning threshold in MB (default: 512)

### State Management (`internal/state/manager.go`)
* **Thread-Safe Device Registry:** Central `StateManager` with `sync.RWMutex` protecting all operations
* **Single Source of Truth:** All device discovery and monitoring funnel through StateManager
* **Device Struct Fields:**
  * `IP` (string): IPv4 address
  * `Hostname` (string): From SNMP or defaults to IP
  * `SysDescr` (string): SNMP sysDescr MIB-II value
  * `LastSeen` (time.Time): Timestamp of last successful ping

**Public Methods:**
* `NewManager(maxDevices int) *Manager`: Factory constructor with device limit
* `Add(device Device)`: Idempotent device insertion with LRU eviction
* `AddDevice(ip string) bool`: Add device by IP only, returns true if new
* `Get(ip string) (*Device, bool)`: Retrieve device by IP
* `GetAll() []Device`: Return copy of all devices (slice, not map)
* `GetAllIPs() []string`: Return slice of all device IPs
* `UpdateLastSeen(ip string)`: Refresh timestamp (called by pingers on successful ping)
* `UpdateDeviceSNMP(ip, hostname, sysDescr string)`: Enrich device with SNMP metadata
* `Prune(olderThan time.Duration) []Device`: Remove stale devices, returns removed list
* `PruneStale(olderThan time.Duration) []Device`: Alias for Prune (new architecture naming)
* `Count() int`: Return current device count

**LRU Eviction:** When `max_devices` limit reached, automatically removes oldest device (by LastSeen timestamp) before adding new one

### ICMP Discovery (`internal/discovery/scanner.go`)
* **Function:** `RunICMPSweep(networks []string, workers int) []string`
* **Worker Pool Pattern:** Concurrent ping sweep with configurable workers (default: 64)
* **CIDR Support:** Expands network ranges (e.g., "192.168.1.0/24") into individual IPs
* **Returns:** Only IPs that responded to ICMP echo request
* **Timeout:** 1 second per ping during discovery
* **Privileged Mode:** Uses `SetPrivileged(true)` for raw ICMP sockets
* **Panic Recovery:** All worker goroutines protected with panic recovery and structured logging
* **Streaming:** IPs streamed directly to job channel without intermediate array allocation

### SNMP Scanning (`internal/discovery/scanner.go`)
* **Function:** `RunSNMPScan(ips []string, snmpConfig *config.SNMPConfig, workers int) []state.Device`
* **Worker Pool Pattern:** Concurrent SNMP queries with configurable workers (default: 32)
* **Timeout:** Configurable via `config.SNMP.Timeout` (default: 5s)
* **Returns:** Slice of `state.Device` structs with SNMP data populated
* **Graceful Failure:** Devices that don't respond to SNMP are logged and skipped, not errors

**SNMP Robustness Features:**
* **`snmpGetWithFallback()` Function:** Tries SNMP Get first, automatically falls back to GetNext if NoSuchInstance error
* **OID Prefix Validation:** Ensures GetNext results are under requested base OID (prevents incorrect fallback data)
* **Type Handling:** `validateSNMPString()` handles both `string` and `[]byte` (OctetString) types
* **Byte Array Conversion:** Converts `[]byte` OctetString values to strings for ASCII/UTF-8 encoded data
* **Error Prevention:** Eliminates "invalid type: expected string, got []uint8" errors

**Queried OIDs:**
* `1.3.6.1.2.1.1.5.0` (sysName): Device hostname
* `1.3.6.1.2.1.1.1.0` (sysDescr): System description
* Both use GetWithFallback for maximum device compatibility

### Continuous Monitoring (`internal/monitoring/pinger.go`)
* **Function:** `StartPinger(ctx context.Context, wg *sync.WaitGroup, device state.Device, interval time.Duration, writer PingWriter, stateMgr StateManager)`
* **Lifecycle:** Runs in dedicated goroutine per device, continues until context cancelled
* **Ticker:** Uses `time.NewTicker(interval)` for consistent ping frequency (default: 2s)
* **IP Validation:** Comprehensive security checks before each ping:
  * Format validation via `net.ParseIP()`
  * Rejects loopback, multicast, link-local, and unspecified addresses
  * Prevents dangerous network scanning
* **Timeout:** 2 second timeout per ping operation
* **State Updates:** Calls `stateMgr.UpdateLastSeen(ip)` on successful ping
* **Metrics Writing:** Calls `writer.WritePingResult(ip, rtt, success)` for all ping attempts
* **Error Handling:** Logs all errors with structured context, continues loop (does not exit on failure)
* **Panic Recovery:** Protected with panic recovery at function entry
* **Graceful Shutdown:** Listens for `ctx.Done()` and exits cleanly, decrements WaitGroup

**Interface Design:**
* `PingWriter` interface: `WritePingResult()` and `WriteDeviceInfo()` methods
* `StateManager` interface: `UpdateLastSeen()` method
* Enables easy mocking for unit tests

### InfluxDB Writer (`internal/influx/writer.go`)
* **High-Performance Batch System:** Lock-free channel-based batching with background flusher goroutine
* **Constructor:** `NewWriter(url, token, org, bucket string, batchSize int, flushInterval time.Duration) *Writer`
* **Health Check:** `HealthCheck() error` - Performs connectivity test, called on startup (fail-fast) and by health endpoint
* **Metrics Tracking:** Atomic counters for successful and failed batch writes

**Batching Architecture:**
* **Channel-Based:** Points queued to buffered channel (capacity: `batchSize * 2`)
* **Background Flusher:** Dedicated goroutine accumulates points and flushes on two triggers:
  1. Batch full (reached `batchSize` points)
  2. Timer fires (after `flushInterval` duration)
* **Non-Blocking Writes:** `WritePingResult()` and `WriteDeviceInfo()` immediately return after queuing point
* **Graceful Shutdown:** `Close()` method drains remaining points and performs final flush
* **Error Monitoring:** Separate goroutine monitors WriteAPI error channel and logs failures with context

**Write Methods:**
* `WritePingResult(ip string, rtt time.Duration, successful bool) error`: Queues ping metrics to batch
  * Measurement: "ping"
  * Tags: "ip", "hostname"
  * Fields: "rtt_ms" (float64), "success" (bool)
* `WriteDeviceInfo(ip, hostname, description string) error`: Queues device metadata to batch
  * Measurement: "device_info"
  * Tags: "ip"
  * Fields: "hostname" (string), "snmp_description" (string)
  * All strings sanitized via `sanitizeInfluxString()` to prevent injection

**Data Sanitization:**
* Removes null bytes, control characters (except \n, \r, \t)
* Validates UTF-8 encoding via `utf8.ValidString()`
* Truncates to 64KB max length

**Metrics Methods:**
* `GetSuccessfulBatches() uint64`: Returns atomic counter of successful batch writes
* `GetFailedBatches() uint64`: Returns atomic counter of failed batch writes

**Performance:** 99% reduction in InfluxDB HTTP requests for deployments with 100+ devices

### Health Check Server (`cmd/netscan/health.go`)
* **HTTP Server:** Non-blocking server on configurable port (default: 8080)
* **Three Endpoints:**
  1. `/health` - Detailed JSON status with full metrics
  2. `/health/ready` - Readiness probe (200 if InfluxDB OK, 503 if unavailable)
  3. `/health/live` - Liveness probe (200 if application running)

**Health Response JSON Structure:**
```go
{
  "status": "healthy|degraded|unhealthy",
  "version": "1.0.0",
  "uptime": "1h23m45s",
  "device_count": 142,
  "active_pingers": 142,  // Accurate count via mutex-protected callback
  "influxdb_ok": true,
  "influxdb_successful": 12543,
  "influxdb_failed": 0,
  "goroutines": 156,
  "memory_mb": 34,
  "timestamp": "2025-10-24T13:00:00Z"
}
```

**Docker Integration:**
* HEALTHCHECK directive in Dockerfile: `wget --spider http://localhost:8080/health/live`
* Docker Compose healthcheck configuration with 30s interval, 3s timeout, 3 retries, 40s start period

**Kubernetes Integration:**
* Compatible with livenessProbe and readinessProbe formats
* Enables automated pod restart on failure and traffic routing control

### Structured Logging (`internal/logger/logger.go`)
* **Library:** zerolog v1.34.0 for zero-allocation performance
* **Setup:** `Setup(debugMode bool)` initializes global logger
* **Log Levels:** Fatal, Error, Warn, Info, Debug (configurable via debugMode)
* **Output Formats:**
  * Production: Machine-parseable JSON to stdout
  * Development: Colored console output when `ENVIRONMENT=development`
* **Common Fields:** All logs include `service: netscan` and `timestamp`
* **Structured Context:** All log messages use context fields (ip, device_count, errors, durations, etc.)

**Usage Pattern Throughout Codebase:**
```go
log.Info().
    Str("ip", "192.168.1.1").
    Dur("rtt", rtt).
    Msg("Ping successful")
```

**Log Level Guidelines:**
* **Fatal:** Configuration errors and startup failures (exits process)
* **Error:** Failed operations requiring investigation (ping failures, write errors)
* **Warn:** Resource limits, configuration warnings, non-critical issues
* **Info:** All major operations (startup, scans, discovery, shutdown)
* **Debug:** Verbose details (pinger lifecycle, SNMP query details)

### Orchestration Tests (`cmd/netscan/orchestration_test.go`)
* **Comprehensive Test Suite:** 527 lines, 11 test functions, 1 benchmark
* **Coverage Areas:**
  * Daily SNMP scheduling with 10 edge cases (valid times, invalid formats, wraparound)
  * Context cancellation and ticker cleanup
  * Pinger reconciliation (6 scenarios for lifecycle management)
  * Multi-ticker concurrent operation validation
  * Parent-child context relationships
  * 24-hour scheduling logic with time wraparound
  * Resource limit enforcement (max pingers)
  * Time parsing with 8 edge cases
  * Concurrent map access patterns (documents mutex necessity)

**Performance Baseline:**
* `BenchmarkPingerReconciliation`: 1000 devices simulation for regression detection
* Fast execution: <2 seconds total for all tests
* Race detection clean: `go test -race ./...` passes

**Test Quality:**
* Realistic scenarios matching production usage
* Clear documentation of expected behavior
* No flaky tests or time-dependent failures

---

## Core Principles & Mandates (Read Before Coding)

These are the rules and best practices derived from production implementation. All new and existing code must follow them.

### Docker & Deployment Mandates

* **Mandate: The container MUST run as root.** This is a non-negotiable requirement for ICMP raw socket access in Linux containers. Do not attempt non-root workarounds for ICMP. This is an accepted security trade-off.
* **Mandate: Docker builds MUST be multi-stage.**
    * Stage 1: Build with the correct Go version (`golang:1.25-alpine`).
    * Stage 2: Runtime with minimal `alpine:latest`.
    * Binaries MUST be stripped (`-ldflags="-w -s"`) to keep the final image small (~15MB).
* **Mandate: Documentation MUST clearly separate Docker and Native deployment.**
    * `README.md` is for Docker (primary).
    * `README_NATIVE.md` is for Native (alternative).
    * Security trade-offs (e.g., Docker root vs. Native `setcap`) MUST be explicitly explained in the docs.

### Configuration Mandates

* **Mandate: All configuration MUST be loadable via environment variables.** Use `os.ExpandEnv()` on the loaded config file. This is the standard for both Docker (`docker-compose.yml` or `.env`) and native (`.env`) deployments.
* **Mandate: Configuration changes MUST be backward-compatible.** New fields must be optional and have sensible defaults. Do not break existing `config.yml` files.
* **Mandate: All example configurations (`config.yml.example`) MUST include prominent warnings** for users to change default values (like network ranges) to match their environment.

### SNMP Mandates

* **Mandate: All new SNMP queries MUST use `snmpGetWithFallback()`.** Direct `snmpGet` calls are not permitted without this wrapper. This ensures compatibility with devices that fail on `.0` OID instances by falling back to `GetNext`.
* **MandATE: All SNMP string processing MUST handle `[]byte` (OctetString).** Use a helper function (like `validateSNMPString()`) to check for both `string` and `[]byte` types and convert as needed to prevent "invalid type" errors.

### Logging & Data Mandates

* **Mandate: All new components MUST include diagnostic logging.** At a minimum, log:
    * Configuration values being used (e.g., "Scanning networks: %v").
    * Entry/exit of major operations.
    * Specific error details with context (e.g., "Ping failed for %s: %v"). Do not silently fail.
* **Mandate: Summary logs MUST report both success and failure counts.** (e.g., "Enriched X devices (failed: Y)").
* **Mandate: The InfluxDB schema MUST remain simple.** Do not add fields that are not actively used for monitoring or queries (e.g., `sysObjectID`). The primary schema is `device_info` with `IP`, `hostname`, and `snmp_description`.
* **Mandate: Log all successful writes to InfluxDB.** Include the device identifier (IP/hostname) in the log message for debugging and confirmation.

### Observability & Monitoring Mandates

* **Mandate: All services MUST expose health status.** Services must provide a way to verify they are running correctly. For network monitoring services like netscan, this includes:
    * Overall service health (running/degraded/down)
    * Dependency status (InfluxDB connectivity)
    * Current operational state (number of active devices, pingers, etc.)
* **Mandate: Critical metrics MUST be tracked.** Track key performance indicators:
    * Active device count
    * Active pinger count
    * Scan duration (ICMP discovery, SNMP scans)
    * InfluxDB write success/failure rates
    * Memory usage
    * Goroutine count
* **Mandate: Health checks MUST be Docker-compatible.** When adding health checks:
    * Implement HTTP endpoint (e.g., `/health`) on a configurable port
    * Return structured JSON with status details
    * Support both readiness and liveness concepts
    * Enable HEALTHCHECK directive in Dockerfile
    * Support Kubernetes probes format

### Security Scanning & Updates

* **Mandate: Dependencies MUST be scanned for vulnerabilities.** Use automated tools:
    * Run `govulncheck` in CI/CD pipeline
    * Scan Docker images with Trivy or similar
    * Address HIGH and CRITICAL CVEs within 30 days
    * Document accepted risks for LOW/MEDIUM issues
* **Mandate: Multi-architecture support SHOULD be considered.** Build for:
    * linux/amd64 (primary)
    * linux/arm64 (for ARM servers, newer Raspberry Pi)
    * linux/arm/v7 (for older Raspberry Pi)
* **Mandate: SNMPv3 support SHOULD be prioritized.** SNMPv2c uses plain-text community strings. For production security:
    * Add SNMPv3 support with authentication and encryption
    * Make SNMPv3 the recommended configuration
    * Keep SNMPv2c for backward compatibility only

---

## Technology Stack

* **Language**: Go 1.25.1 (module: github.com/kljama/netscan, go.mod specifies go 1.25.1)
* **Platform**: linux-amd64 (exclusively, no multi-architecture support currently)
* **Primary Dependencies** (from go.mod):
    * `gopkg.in/yaml.v3 v3.0.1` - YAML configuration parsing
    * `github.com/gosnmp/gosnmp v1.42.1` - SNMPv2c protocol implementation
    * `github.com/prometheus-community/pro-bing v0.7.0` - ICMP ping implementation with raw socket support
    * `github.com/influxdata/influxdb-client-go/v2 v2.14.0` - InfluxDB v2 client with WriteAPI
    * `github.com/rs/zerolog v1.34.0` - Zero-allocation structured logging (JSON and console)
* **Standard Library Usage**:
    * `sync.RWMutex` - Critical for StateManager and activePingers map protection
    * `sync.WaitGroup` - Pinger goroutine lifecycle tracking
    * `context.Context` - Cancellation propagation and graceful shutdown
    * `time.Ticker` - Four independent event loops
    * `net` - IP address validation and parsing
    * `flag` - CLI argument parsing
* **Infrastructure**:
    * **Docker:** Multi-stage Dockerfile with golang:1.25-alpine builder, alpine:latest runtime
    * **InfluxDB:** v2.7 container for time-series metrics storage
    * **Alpine Linux:** Base image for minimal attack surface (~15MB final image)
* **Build Tools**:
    * `go build` with CGO_ENABLED=0 for static binary
    * `-ldflags="-w -s"` for stripped binary (smaller size)
    * `GOOS=linux GOARCH=amd64` for linux-amd64 target
* **Testing**:
    * `go test` - Standard testing framework
    * `go test -race` - Race condition detection (all tests pass)
    * `go test -cover` - Code coverage reporting

---

## Testing & Validation Approach

### Build & Test
* Run `go build ./cmd/netscan` frequently during development
* Run `go test ./...` to validate all tests pass
* Run `go test -race ./...` to detect race conditions
* All tests must pass before committing changes

### Manual Validation
* Test with actual config files to verify config loading
* Test with diverse SNMP devices to verify compatibility
* Monitor logs to ensure operations are working correctly
* Verify InfluxDB writes by querying the database

### CI/CD Requirements
* `./netscan --help` must work (flag support required)
* All unit tests must pass
* All integration tests must pass (if applicable)
* Race detection must be clean: `go test -race ./...`
* Build must succeed with no warnings
* Security scanning must pass (govulncheck, Trivy)
* Performance benchmarks must not regress
* Docker Compose workflow validates full stack deployment
* Workflow creates config.yml from template and runs `docker compose up`
* Multi-architecture Docker images must build successfully

### Testing Mandates (Additions)

* **Mandate: Orchestration logic MUST be tested.** The main ticker orchestration in `cmd/netscan/main.go` is critical and must have:
    * Integration tests for ticker lifecycle
    * Tests for graceful shutdown behavior
    * Tests for signal handling
    * Tests for pinger reconciliation logic
* **Mandate: Performance benchmarks MUST exist for hot paths.** Track performance over time:
    * Benchmark ICMP sweeps
    * Benchmark SNMP scans
    * Benchmark state manager operations under load
    * Benchmark InfluxDB write operations
* **Mandate: Resource limit enforcement MUST be tested.** Verify that:
    * `max_devices` limit is enforced
    * `max_concurrent_pingers` limit is enforced
    * Memory limits trigger warnings
    * Device eviction works correctly

---

## 📋 How to Add a New Feature (Example: New Scanner)

Follow this workflow when adding a new, recurring task:

1.  **Config:** Add new parameters to `internal/config/config.go` and `config.yml.example`. (e.g., `NewScannerInterval time.Duration`). Ensure env var support and a sensible default.
2.  **Logic:** Create the core logic in its own package (e.g., `internal/newscanner/scanner.go`). Adhere to all "Resilience First" principles.
3.  **State (If needed):** Add thread-safe methods to `internal/state/manager.go` to store or retrieve any new data.
4.  **Database (If needed):** Add a new method to `internal/influx/writer.go` to persist the new data (e.g., `WriteNewData(...)`). Remember to include timeouts, sanitization, and logging.
5.  **Orchestration:** Add a new Ticker loop in `cmd/netscan/main.go` to run the new scanner at its configured interval. Ensure it's non-blocking and respects graceful shutdown.
6.  **Testing:** Add unit tests for the new logic and validation rules. Use interfaces for mocking.
7.  **Documentation:** Update this file and the `README.md` with the new feature.

---

## Production Readiness Checklist

Before deploying to production, ensure:

### Essential (MUST HAVE)
- [ ] Health check endpoint implemented and tested
- [ ] All critical metrics being tracked
- [ ] Structured logging in place
- [ ] Resource limits configured appropriately
- [ ] InfluxDB connection resilience tested
- [ ] Graceful shutdown working correctly
- [ ] Configuration validation in place
- [ ] Security scanning passing (no HIGH/CRITICAL CVEs)

### Recommended (SHOULD HAVE)
- [ ] Integration tests passing
- [ ] Performance benchmarks established
- [ ] Multi-architecture builds available
- [ ] Kubernetes manifests available (if applicable)
- [ ] Monitoring dashboard configured (Grafana)
- [ ] Alert rules configured (InfluxDB/Grafana)
- [ ] Operational runbook documented
- [ ] Disaster recovery plan documented

### Optional (NICE TO HAVE)
- [ ] SNMPv3 support enabled
- [ ] State persistence configured
- [ ] IPv6 support enabled
- [ ] Device grouping/tagging implemented
- [ ] Webhook alerting configured
- [ ] Secrets management integrated

---

## Common Implementation Patterns

### Adding a New Ticker
```go
// 1. Add configuration parameter
type Config struct {
    NewScannerInterval time.Duration `yaml:"new_scanner_interval"`
}

// 2. Create ticker in main.go
newScannerTicker := time.NewTicker(cfg.NewScannerInterval)
defer newScannerTicker.Stop()

// 3. Add to main event loop
case <-newScannerTicker.C:
    // Your scan logic here
    log.Println("Starting new scanner...")
```

### Adding a New Metric
```go
// 1. Define metric in metrics package
var DevicesScanned = prometheus.NewCounterVec(
    prometheus.CounterOpts{
        Name: "netscan_devices_scanned_total",
        Help: "Total number of devices scanned",
    },
    []string{"scan_type"},
)

// 2. Register metric
func init() {
    prometheus.MustRegister(DevicesScanned)
}

// 3. Increment metric
DevicesScanned.WithLabelValues("icmp").Inc()
```

### Adding a New State Field
```go
// 1. Update Device struct
type Device struct {
    IP       string
    NewField string  // Add your field
    LastSeen time.Time
}

// 2. Add update method
func (m *Manager) UpdateNewField(ip, value string) {
    m.mu.Lock()
    defer m.mu.Unlock()
    if dev, exists := m.devices[ip]; exists {
        dev.NewField = value
    }
}

// 3. Update tests
func TestUpdateNewField(t *testing.T) {
    // Test the new method
}
```

---

## Performance Optimization Guidelines

### Batch Operations
* **InfluxDB Writes:** Batch multiple data points before writing to InfluxDB
    * Default batch size: 100 points
    * Default flush interval: 5 seconds
    * Configurable via `influxdb.batch_size` and `influxdb.flush_interval`
* **SNMP Queries:** Group OIDs when querying same device to reduce round-trips
* **Network Operations:** Use worker pools efficiently (already implemented)

### Resource Management
* **Adaptive Workers:** Consider auto-tuning worker counts based on:
    * Available CPU cores (`runtime.NumCPU()`)
    * Network latency
    * Current load
* **Circuit Breakers:** For devices that consistently fail:
    * Stop trying after N consecutive failures
    * Exponential backoff before retry
    * Remove from active monitoring (optionally)

### Memory Optimization
* **Device Eviction:** Already implemented (LRU-style eviction)
* **Goroutine Pooling:** Reuse goroutines where possible
* **Buffer Sizing:** Use appropriate channel buffer sizes (currently 256)

---

## Upgrade and Migration Guidelines

### Version Compatibility
* **Configuration backward compatibility:** New fields must have sensible defaults
* **State format changes:** Must support migration from previous version
* **Breaking changes:** Require major version bump and migration guide

### Upgrade Procedure
1. **Review changelog** for breaking changes
2. **Backup current state** (if persistence enabled)
3. **Update configuration** with new fields (optional)
4. **Test in staging** environment first
5. **Rolling update** in production (if multiple instances)
6. **Monitor logs** for errors after upgrade
7. **Rollback plan** ready if issues occur

### Migration Scripts
* Provide migration scripts for breaking changes
* Document manual migration steps clearly
* Test migration with realistic data volumes

---

### Main Orchestration (`cmd/netscan/main.go`)

**Multi-Ticker Architecture** - Four independent concurrent event loops:

**Initialization Sequence:**
1. Parse `-config` CLI flag (default: "config.yml")
2. Initialize structured logging with zerolog (`logger.Setup(false)`)
3. Load and validate configuration (`config.LoadConfig()`, `config.ValidateConfig()`)
4. Create StateManager with device limit (`state.NewManager(cfg.MaxDevices)`)
5. Create InfluxDB writer with batching (`influx.NewWriter()` with batchSize and flushInterval)
6. Perform InfluxDB health check (fail-fast on error: `log.Fatal()`)
7. Initialize `activePingers map[string]context.CancelFunc` for pinger lifecycle management
8. Initialize `pingersMu sync.Mutex` to protect activePingers map (CRITICAL for thread safety)
9. Initialize `pingerWg sync.WaitGroup` to track all pinger goroutines
10. Start health check HTTP server (`NewHealthServer()` with accurate pinger count callback)
11. Setup signal handling (SIGINT, SIGTERM) for graceful shutdown
12. Create main context with cancel function
13. Run initial ICMP discovery scan before entering main loop

**Ticker 1: ICMP Discovery Loop**
* **Interval:** `cfg.IcmpDiscoveryInterval` (e.g., 5m)
* **Purpose:** Find new responsive devices on the network
* **Process:**
  1. Log memory usage check
  2. Run `discovery.RunICMPSweep(cfg.Networks, cfg.IcmpWorkers)`
  3. For each responsive IP:
     * Call `stateMgr.AddDevice(ip)` - returns true if new device
     * If new device:
       * Log discovery with IP
       * Launch non-blocking goroutine for immediate SNMP scan:
         * Call `discovery.RunSNMPScan([ip], &cfg.SNMP, cfg.SnmpWorkers)`
         * Update StateManager with SNMP data via `UpdateDeviceSNMP()`
         * Write device info to InfluxDB via `writer.WriteDeviceInfo()`
         * Log success or failure with structured context
* **Non-Blocking:** SNMP scans run in separate goroutines, discovery loop continues immediately
* **Panic Recovery:** All goroutines protected with defer panic recovery

**Ticker 2: Daily SNMP Scan Loop**
* **Schedule:** `cfg.SNMPDailySchedule` (HH:MM format, e.g., "02:00")
* **Implementation:** `createDailySNMPChannel()` calculates next run time and returns channel
* **Time Handling:**
  * Parses HH:MM format with validation (00:00 to 23:59)
  * Calculates duration until next occurrence (handles day wraparound)
  * Logs next scheduled run time
  * Invalid format falls back to "02:00" with warning
* **Process:**
  1. Retrieve all device IPs via `stateMgr.GetAllIPs()`
  2. Run `discovery.RunSNMPScan(allIPs, &cfg.SNMP, cfg.SnmpWorkers)`
  3. For each device returned:
     * Update StateManager via `UpdateDeviceSNMP()`
     * Write device info to InfluxDB
  4. Log summary with success/failure counts
* **Disable Option:** Empty string for `snmp_daily_schedule` disables scheduled scans (only immediate scans on discovery)

**Ticker 3: Pinger Reconciliation Loop**
* **Interval:** Fixed 5 seconds
* **Purpose:** Ensure activePingers map matches StateManager device list
* **Critical:** Uses `pingersMu.Lock()` for entire reconciliation to prevent race conditions
* **Process:**
  1. Get all device IPs from StateManager
  2. Convert to map[string]bool for fast lookup
  3. **Start new pingers:**
     * For each IP in StateManager not in activePingers:
       * Create child context: `pingerCtx, pingerCancel := context.WithCancel(mainCtx)`
       * Store cancel function: `activePingers[ip] = pingerCancel`
       * Increment WaitGroup: `pingerWg.Add(1)`
       * Launch pinger: `go monitoring.StartPinger(pingerCtx, &pingerWg, device, cfg.PingInterval, writer, stateMgr)`
       * Log pinger start with IP
  4. **Stop old pingers:**
     * For each IP in activePingers not in StateManager:
       * Call cancel function: `cancelFunc()`
       * Remove from map: `delete(activePingers, ip)`
       * Log pinger stop with IP
* **Thread Safety:** Full mutex lock ensures no concurrent map access

**Ticker 4: State Pruning Loop**
* **Interval:** Fixed 1 hour
* **Purpose:** Remove devices not seen in last 24 hours
* **Process:**
  1. Log pruning operation
  2. Call `stateMgr.PruneStale(24 * time.Hour)`
  3. Reconciliation loop will automatically stop pingers for removed devices

**Graceful Shutdown Sequence:**
1. Signal received (SIGINT or SIGTERM)
2. Log shutdown message
3. Call `stop()` to cancel main context
4. Stop all four tickers
5. Acquire `pingersMu` lock
6. Iterate all active pingers and call their cancel functions
7. Release `pingersMu` lock
8. Call `pingerWg.Wait()` to wait for all pinger goroutines to exit
9. Call `writer.Close()` to flush remaining batched points
10. Log "Shutdown complete"
11. Return from main

**Concurrency Model:**
* All four tickers run independently in main select loop
* Pinger goroutines run independently per device
* Mutex protection on activePingers map
* WaitGroup for pinger lifecycle tracking
* Context-based cancellation for clean shutdown
* Panic recovery on all goroutines

**Memory Monitoring:**
* Periodic check via `runtime.ReadMemStats()`
* Warning logged if `memory_mb` exceeds `cfg.MemoryLimitMB`
* Does not stop service, only warns

---

## Future Work / Deferred Tasks

The following features and improvements have been intentionally deferred for future PRs to keep the current implementation focused and maintainable. These represent complex scalability and security enhancements that require careful design and testing.

### Advanced Scalability Features (Deferred)

**1. Rate Limiting & Circuit Breakers**
* **Current State:** No rate limiting beyond worker pool constraints
* **Future Enhancement:**
  * Implement per-device circuit breaker pattern for consistently failing devices
  * Add configurable backoff strategies (exponential, linear)
  * Automatic device suspension after N consecutive failures
  * Re-enable suspended devices on configurable schedule
* **Rationale for Deferral:** Current worker pool pattern provides sufficient rate control for typical deployments (100-1000 devices). Circuit breakers add complexity and require extensive testing with various failure modes.

**2. SNMP Connection Pooling**
* **Current State:** New SNMP connection created for each query
* **Future Enhancement:**
  * Implement connection pool with configurable size
  * Connection reuse across multiple SNMP queries to same device
  * Automatic connection cleanup and health checks
  * Per-device connection affinity
* **Rationale for Deferral:** SNMP queries are infrequent (daily scheduled scan + on-discovery). Connection overhead is minimal for current use cases. Connection pooling adds significant complexity for marginal benefit at current scale.

**3. Enhanced Context Propagation**
* **Current State:** Context used for cancellation, timeouts handled per-operation
* **Future Enhancement:**
  * Request-scoped contexts with tracing IDs for full operation lifecycle
  * Distributed tracing integration (OpenTelemetry)
  * Correlation of logs across goroutines for single discovery operation
  * Context-based request cancellation propagation through entire stack
* **Rationale for Deferral:** Current structured logging provides sufficient debugging capability. Distributed tracing is overkill for single-process architecture. This becomes valuable when scaling to multi-instance deployments.

### Platform & Compatibility Features (Deferred)

**4. Multi-Architecture Support**
* **Current State:** linux-amd64 only
* **Future Enhancement:**
  * Build and test for linux/arm64 (ARM servers, newer Raspberry Pi)
  * Build and test for linux/arm/v7 (older Raspberry Pi)
  * CI/CD pipeline for multi-architecture Docker images
  * Architecture-specific performance tuning
* **Rationale for Deferral:** Primary deployment target is x86_64 servers. ARM support requires access to ARM hardware for testing and performance validation. Adds CI/CD complexity.

**5. SNMPv3 Support**
* **Current State:** SNMPv2c only (plain-text community strings)
* **Future Enhancement:**
  * Add SNMPv3 with authentication (MD5, SHA)
  * Add SNMPv3 with encryption (DES, AES)
  * Configuration migration guide for SNMPv2c → SNMPv3
  * Make SNMPv3 the recommended default
* **Rationale for Deferral:** SNMPv2c is widely supported and sufficient for trusted networks. SNMPv3 implementation requires careful credential management and testing with diverse device implementations. Security improvement is significant but not critical for isolated monitoring networks.

**6. IPv6 Support**
* **Current State:** IPv4 only
* **Future Enhancement:**
  * Dual-stack IPv4/IPv6 discovery and monitoring
  * IPv6 CIDR range expansion
  * IPv6 address validation
  * Separate worker pools for IPv4 and IPv6 (different network characteristics)
* **Rationale for Deferral:** Most enterprise networks still primarily use IPv4 for management interfaces. IPv6 support requires significant testing and dual-stack network infrastructure.

### Operational Features (Deferred)

**7. State Persistence**
* **Current State:** In-memory only, devices lost on restart
* **Future Enhancement:**
  * Periodic state snapshots to disk (JSON or SQLite)
  * State restoration on startup
  * Configurable snapshot interval
  * Graceful degradation if state file corrupted
* **Rationale for Deferral:** State is easily rebuilt via ICMP discovery (5 minutes default). Persistence adds complexity and introduces new failure modes. Benefit is minimal for typical restart scenarios.

**8. Device Grouping & Tagging**
* **Current State:** Flat device list, no grouping
* **Future Enhancement:**
  * Configurable device groups (network segments, device types, locations)
  * Tags extracted from SNMP data or configuration
  * Group-based alerting and reporting
  * InfluxDB tag propagation for efficient querying
* **Rationale for Deferral:** Current flat structure is simple and works well for small-to-medium deployments. Grouping requires UI or advanced configuration syntax. Can be implemented in visualization layer (Grafana) for now.

**9. Webhook Alerting**
* **Current State:** Metrics stored in InfluxDB, no alerting
* **Future Enhancement:**
  * Configurable webhook endpoints for alerts
  * Alert conditions (device down > N minutes, discovery failures, etc.)
  * Alert deduplication and rate limiting
  * Slack, PagerDuty, generic webhook support
* **Rationale for Deferral:** InfluxDB + Grafana provide robust alerting capabilities. In-process alerting duplicates functionality and adds complexity. Better handled by dedicated monitoring stack.

**10. Prometheus Metrics Export**
* **Current State:** Health endpoint with JSON metrics
* **Future Enhancement:**
  * `/metrics` endpoint in Prometheus format
  * Prometheus client library integration
  * Standard metrics (device_count, ping_success_rate, etc.)
  * Custom metric registration API
* **Rationale for Deferral:** InfluxDB + Grafana provide full metrics solution. Prometheus support adds dependency and complexity. Health endpoint JSON is sufficient for basic monitoring.

### Notes on Deferral Strategy

These features are deferred, not rejected. They represent legitimate improvements that may be implemented when:
1. Project scale increases beyond current architecture's capabilities
2. Security requirements become more stringent (SNMPv3)
3. Deployment targets expand (ARM, IPv6)
4. Operational complexity justifies additional automation

The current implementation prioritizes:
* **Simplicity:** Fewer moving parts, easier to debug
* **Reliability:** Well-tested core functionality
* **Performance:** Proven to handle 1000+ devices efficiently
* **Maintainability:** Clean architecture, comprehensive tests

Adding these features prematurely would increase code complexity, testing burden, and potential failure modes without proportional benefit at current scale.
