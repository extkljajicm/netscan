# GitHub Copilot Instructions for Project: netscan

## Project Goal

`netscan` is a Go-based network monitoring service with a decoupled, multi-ticker architecture designed for containerized deployment via Docker Compose, with native deployment as an alternative option. The service follows this workflow:

1.  **Periodic ICMP Discovery:** Scan configured network ranges to find new, active devices.
2.  **Continuous ICMP Monitoring:** For *every* device found, initiate an immediate and continuous high-frequency (e.g., 1-second) ICMP ping to track real-time uptime.
3.  **Dual-Trigger SNMP Scanning:**
    * **Initial:** Perform an SNMP query *immediately* after a device is first discovered.
    * **Scheduled:** Perform a full SNMP scan on *all* known-alive devices at a configurable daily time (e.g., 02:00 AM).
4.  **Resilient Data Persistence:** Write all monitoring (ICMP) and discovery (SNMP) data to InfluxDB, with robust protections against network failures, data corruption, and database overload.

## Deployment Architecture

### Docker Deployment (Primary)
- **Recommended approach** for ease of deployment and consistency
- Multi-stage Dockerfile with minimal Alpine Linux runtime (~15MB)
- Docker Compose orchestrates netscan + InfluxDB stack
- **Security Note:** Container runs as root (required for ICMP raw socket access in Linux containers)
- Host networking mode for direct network access
- Environment variable support via docker-compose.yml or optional .env file
- See `README.md` for Docker deployment instructions

### Native Deployment (Alternative)
- For maximum security with non-root service user
- Uses dedicated `netscan` system user with no shell access
- CAP_NET_RAW capability via setcap (no root required)
- Systemd service with security restrictions
- See `README_NATIVE.md` for native deployment instructions

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

## Core Features (Implemented)

* **Configuration:** Load parameters from `config.yml` with full environment variable support.
    * `icmp_discovery_interval`: How often to scan for *new* devices (e.g., "5m").
    * `ping_interval`: How often to ping *known* devices (e.g., "1s").
    * `snmp_daily_schedule`: What time to run the full SNMP scan (e.g., "02:00").
    * `health_check_port`: Port for health check HTTP server (default: 8080).
    * `batch_size`: InfluxDB batch size for performance (default: 100).
    * `flush_interval`: InfluxDB batch flush interval (default: "5s").
    * `discovery_interval`: Optional for backward compatibility (defaults to 4h if omitted).
* **State Management:** A thread-safe, in-memory registry (`StateManager`) is the "single source of truth" for all known devices. It supports adding new devices, updating/enriching existing ones, pruning stale devices, and reporting device counts for monitoring.
* **Decoupled ICMP Discovery:** A dedicated scanner that runs on `icmp_discovery_interval`, sweeps network ranges, and feeds *new* IPs to the `StateManager`.
* **Decoupled SNMP Scanning:** A dedicated, concurrent scanner that can be triggered in two ways:
    1.  On-demand (for a single, newly-discovered IP).
    2.  Scheduled (for *all* IPs currently in the `StateManager`).
* **Continuous Monitoring:** A "reconciliation loop" that ensures *every* device in the `StateManager` has a dedicated, continuous `pinger` goroutine running. This loop also handles device removal, gracefully stopping the associated goroutine.
* **Health Check Endpoint:** HTTP server with three endpoints for production monitoring:
    * `/health` - Detailed JSON status with device count, memory, goroutines, and InfluxDB connectivity
    * `/health/ready` - Readiness probe (returns 503 if InfluxDB unavailable)
    * `/health/live` - Liveness probe (returns 200 if application running)
    * Docker HEALTHCHECK and Kubernetes probe support
* **Batch InfluxDB Writes:** Performance-optimized batching system:
    * Points accumulated in memory up to configurable batch size
    * Automatic flush when batch full or on timer interval
    * Background flusher goroutine with graceful shutdown
    * 99% reduction in InfluxDB requests for large deployments
* **Structured Logging:** Machine-parseable JSON logs with zerolog:
    * Context-rich logging with IP addresses, device counts, error details
    * Production JSON format for log aggregation (ELK, Splunk, etc.)
    * Development-friendly colored console output
    * Zero-allocation performance
* **Security Scanning:** Automated vulnerability detection in CI/CD:
    * govulncheck for Go dependency scanning
    * Trivy for filesystem and Docker image scanning
    * GitHub Security integration with SARIF uploads
    * Blocks deployment on CRITICAL/HIGH vulnerabilities
* **Comprehensive Testing:** Test suite covering critical orchestration logic:
    * 11 test functions for ticker lifecycle, shutdown, and reconciliation
    * Performance benchmarks for regression detection
    * Race detection clean
* **Resilience & Security (Mandatory):**
    * **Timeouts:** Aggressive `context.WithTimeout` applied to *all* external calls: ICMP ping, SNMP queries, and InfluxDB writes.
    * **InfluxDB Protection:** InfluxDB **health check** on startup, **batched writes** with rate limiting, and **strict data sanitization/validation** before every write.
    * **Error Handling:** A failed ping or SNMP query is logged but does *not* crash the pinger or the application.
    * **Security Scanning:** Automated vulnerability detection in CI/CD pipeline prevents vulnerable code deployment.

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

* **Language**: Go 1.25+ (updated from 1.21 - ensure Dockerfile uses golang:1.25-alpine)
* **Key Libraries**:
    * `gopkg.in/yaml.v3` (Config)
    * `github.com/gosnmp/gosnmp` (SNMP)
    * `github.com/prometheus-community/pro-bing` (ICMP)
    * `github.com/influxdata/influxdb-client-go/v2` (InfluxDB)
    * `sync.RWMutex` (Critical for `StateManager` and `activePingers` map)
    * `golang.org/x/time/rate` (Rate limiting - already used)
* **Recommended Additional Libraries**:
    * `github.com/prometheus/client_golang` (Metrics collection)
    * `github.com/rs/zerolog` or `go.uber.org/zap` (Structured logging)
    * `golang.org/x/vuln/cmd/govulncheck` (Vulnerability scanning)
* **Deployment**:
    * Docker + Docker Compose (primary deployment method)
    * InfluxDB v2.7 container
    * Alpine Linux base image

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

## Architecture & Implementation Details

### Configuration (`internal/config/config.go`)

**Implemented:**
* `Config` struct includes:
    * `ICMPDiscoveryInterval time.Duration` (e.g., "5m")
    * `PingInterval time.Duration` (e.g., "1s")
    * `SNMPDailySchedule string` (e.g., "02:00")
    * `HealthCheckPort int` (default: 8080)
    * `InfluxDBConfig.BatchSize int` (default: 100)
    * `InfluxDBConfig.FlushInterval time.Duration` (default: "5s")
    * All resource limits (`max_devices`, `max_concurrent_pingers`, etc.)
    * `DiscoveryInterval time.Duration` (optional for backward compatibility, defaults to 4h)
* Validation for `SNMPDailySchedule` (must be parseable as `HH:MM`, range 00:00 to 23:59)
* Better error messages for invalid duration fields

### Health Check Server (`cmd/netscan/health.go`)

**Implemented:**
* HTTP server providing three endpoints for production monitoring:
    * `/health` - Detailed JSON status with device count, memory, goroutines, InfluxDB connectivity, uptime
    * `/health/ready` - Readiness probe (200 if InfluxDB OK, 503 if unavailable)
    * `/health/live` - Liveness probe (200 if application running)
* Non-blocking HTTP server running on configurable port
* Docker HEALTHCHECK directive integration
* Kubernetes livenessProbe and readinessProbe support
* JSON-formatted health data with comprehensive metrics

### State Manager (`internal/state/manager.go`)

**Implemented:**
* Central hub, fully thread-safe with `sync.RWMutex`
* `Device` struct stores: `IP`, `Hostname`, `SysDescr`, and `LastSeen time.Time`
    * **Note:** `SysObjectID` was removed as it's not needed for monitoring
* Methods:
    * `AddDevice(ip string) bool`: Adds a device. Returns `true` if it was new.
    * `UpdateDeviceSNMP(ip, hostname, sysDescr string)`: Enriches an existing device with SNMP data.
    * `UpdateLastSeen(ip string)`: Called by pingers to keep a device alive.
    * `GetAllIPs() []string`: Returns all IPs for the daily SNMP scan.
    * `Count() int`: Returns current device count for monitoring.
    * `PruneStale(olderThan time.Duration)`: Removes devices not seen recently.
    * `Add(ip string)`: Legacy method maintained for compatibility.
    * `Prune(olderThan time.Duration)`: Alias for PruneStale.
* `maxDevices` limit and eviction logic fully implemented

### InfluxDB Writer (`internal/influx/writer.go`)

**Implemented:**
* **Batch Write System:** High-performance batching with background flusher
* `NewWriter(...)`: Performs **Health Check** and returns an error on failure, accepts `batchSize` and `flushInterval` parameters
* **Batching Features:**
    * Points accumulated in memory up to configurable batch size (default: 100)
    * Automatic flush when batch full or on timer interval (default: 5s)
    * Background `backgroundFlusher()` goroutine for periodic flushing
    * `addToBatch()` method queues points and triggers flush when needed
    * Graceful shutdown flushes remaining points on `Close()`
* `WritePingResult(...)`: Adds points to batch, never blocks
* `WriteDeviceInfo(ip, hostname, description string)`:
    * Adds points to batch with **data sanitization** on all string fields
    * Simplified signature (removed redundant snmp_name and snmp_sysid fields)
* **Performance:** 99% reduction in InfluxDB requests for large deployments
* Schema simplified to essential fields only (IP, hostname, snmp_description)

### Scanners (`internal/discovery/scanner.go`)

**Implemented:**
* Two clear, concurrent functions for decoupled operations:

**`RunICMPSweep(networks []string, workers int) []string`:**
* Uses a worker pool to ping all IPs in the CIDR ranges
* **Returns only the IPs that responded**
* CIDR expansion limits (e.g., max /16) from `SECURITY_IMPROVEMENTS.md` retained

**`RunSNMPScan(ips []string, snmpConfig *config.SNMPConfig, workers int) []state.Device`:**
* Uses a worker pool to run SNMP queries against the provided list of IPs
* Applies timeouts (`config.Timeout`) to each query
* Gracefully handles hosts that don't respond to SNMP (logs error, continues)
* Returns a list of `state.Device` structs with SNMP data filled in
* **SNMP Robustness Features:**
    * `snmpGetWithFallback()` function: Tries Get first, falls back to GetNext if NoSuchInstance error occurs
    * Validates OID prefixes to ensure GetNext results are under requested base OID
    * Better compatibility with diverse device types and SNMP implementations
* **SNMP Type Handling:**
    * `validateSNMPString()` handles both `string` and `[]byte` types
    * Converts byte arrays (OctetString values) to strings for ASCII/UTF-8 encoded values
    * Prevents "invalid type: expected string, got []uint8" errors

### Structured Logging (`internal/logger/logger.go`)

**Implemented:**
* Centralized logging configuration using zerolog
* `Setup(debugMode bool)` - Initializes global logger with appropriate settings
* `Get()` - Returns logger with context
* `With(key, value)` - Returns logger with additional context
* **Features:**
    * Machine-parseable JSON logs for production
    * Colored console output for development
    * Zero-allocation performance
    * Configurable log levels (Debug, Info, Warn, Error, Fatal)
    * Adds service name ("netscan") and timestamp to all logs
* **Usage throughout application:**
    * All log messages include structured context fields (IP, device counts, errors, durations)
    * Fatal for configuration errors and startup failures
    * Error for failed operations that should be investigated
    * Warn for resource limits and configuration warnings
    * Info for all major operations (startup, scans, discovery, shutdown)
    * Debug for verbose operations (pinger lifecycle, SNMP details)

### Continuous Pinger (`internal/monitoring/pinger.go`)

**Implemented:**
* `StartPinger(ctx context.Context, wg *sync.WaitGroup, device state.Device, interval time.Duration, writer PingWriter, stateMgr StateManager)`:
    * Runs in its *own goroutine* for *one* device
    * Loops on a `time.NewTicker(interval)`
    * Inside the loop:
        1.  Perform a ping (with a short timeout)
        2.  Call `writer.WritePingResult(...)` with the outcome (batched)
        3.  If successful, call `stateMgr.UpdateLastSeen(device.IP)`
        4.  If ping or write fails, **log the error with structured context and continue the loop** (does not exit)
    * Listens for `ctx.Done()` to exit gracefully
    * Uses interface types (`PingWriter`, `StateManager`) for better testability
    * Properly tracks shutdown with WaitGroup

### Orchestration Tests (`cmd/netscan/orchestration_test.go`)

**Implemented:**
* Comprehensive test suite (527 lines, 11 test functions, 1 benchmark)
* **Test Coverage:**
    * `TestCreateDailySNMPChannel` - Daily SNMP scheduling with 10 edge cases
    * `TestGracefulShutdown` - Context cancellation and ticker cleanup verification
    * `TestPingerReconciliation` - 6 scenarios for pinger lifecycle management
    * `TestTickerCoordination` - Multi-ticker concurrent operation validation
    * `TestContextCancellationPropagation` - Parent-child context relationships
    * `TestDailySNMPTimeCalculation` - 24-hour scheduling logic with wraparound
    * `TestMaxPingersLimit` - Resource limit enforcement verification
    * `TestCreateDailySNMPChannelTimeParsing` - 8 time parsing edge cases
    * `TestPingerMapConcurrency` - Documents mutex protection necessity
    * `BenchmarkPingerReconciliation` - Performance baseline with 1000 devices
* **Features:**
    * All tests pass without race conditions
    * Fast execution (<2 seconds total)
    * Realistic test scenarios matching production usage
    * Clear documentation of expected behavior
    * Performance regression detection

### Orchestration (`cmd/netscan/main.go`)

**Implemented - Multi-Ticker Architecture:**

**1. Initialization:**
* Load and validate config (with `-config` flag support)
* Init structured logging with zerolog
* Init `StateManager`
* Init `InfluxWriter` with **fail fast** if health check fails and **batch write support**
* Start health check HTTP server on configurable port
* Setup signal handling for graceful shutdown (using a main `context.Context`)
* `activePingers := make(map[string]context.CancelFunc)`
* `pingersMu sync.Mutex` (**CRITICAL**: Used for *all* access to `activePingers`)
* `var pingerWg sync.WaitGroup` for tracking all pinger goroutines

**2. Ticker 1: ICMP Discovery Loop** (Runs every `config.ICMPDiscoveryInterval`, e.g., 5m):
* Logs: `log.Info().Strs("networks", networks).Msg("Starting ICMP discovery scan")`
* `responsiveIPs := discovery.RunICMPSweep(...)`
* For each `responsiveIP`:
    * `isNew := stateMgr.AddDevice(ip)`
    * If `isNew`:
        * Logs: `log.Info().Str("ip", ip).Msg("New device found, performing initial SNMP scan")`
        * Triggers *immediate, non-blocking* scan in goroutine
        * SNMP scan results update StateManager and write to InfluxDB (batched)
        * Logs success or failure with structured context

**3. Ticker 2: Daily SNMP Scan Loop** (Runs at `config.SNMPDailySchedule`, e.g., "02:00"):
* Calculates next run time using time parsing
* Logs: `log.Info().Msg("Starting daily full SNMP scan")`
* `allIPs := stateMgr.GetAllIPs()`
* `snmpDevices := discovery.RunSNMPScan(allIPs, ...)`
* Updates StateManager with SNMP data
* Writes device info to InfluxDB (batched)
* Logs: `log.Info().Int("enriched", success).Int("failed", failed).Msg("Daily SNMP scan complete")`

**4. Ticker 3: Pinger Reconciliation Loop** (Runs every 5 seconds):
* Ensures `activePingers` map matches `StateManager`
* `pingersMu.Lock()` (Lock for entire reconciliation)
* `currentStateIPs := stateMgr.GetAllIPs()` converted to set
* **Start new pingers:**
    * For each IP in state not in `activePingers`:
        * Logs: `log.Info().Str("ip", ip).Msg("Starting continuous pinger")`
        * Creates `pingerCtx, pingerCancel := context.WithCancel(mainCtx)`
        * `activePingers[ip] = pingerCancel`
        * `pingerWg.Add(1)`
        * `go monitoring.StartPinger(pingerCtx, &pingerWg, ...)`
* **Stop old pingers:**
    * For each IP in `activePingers` not in state:
        * Logs: `log.Info().Str("ip", ip).Msg("Stopping continuous pinger for stale device")`
        * Calls `cancelFunc()`
        * `delete(activePingers, ip)`
* `pingersMu.Unlock()`

**5. Ticker 4: State Pruning Loop** (Runs every 1 hour):
* Logs: `log.Info().Msg("Pruning stale devices")`
* `stateMgr.PruneStale(24 * time.Hour)`

**6. Health Check Server:**
* Runs on configurable port (default: 8080)
* Three endpoints: `/health`, `/health/ready`, `/health/live`
* Non-blocking HTTP server
* Docker HEALTHCHECK and Kubernetes probe support

**7. Graceful Shutdown:**
* When `mainCtx` is canceled (by SIGINT/SIGTERM):
    * Stop all tickers
    * `pingersMu.Lock()`, iterate `activePingers`, call `cancelFunc()` for all
    * `pingersMu.Unlock()`
    * `pingerWg.Wait()` to wait for all pingers to exit
    * Flush remaining batched InfluxDB writes
    * Close InfluxDB client
    * Logs: `log.Info().Msg("Shutdown complete")`

**Key Implementation Notes:**
* All four tickers run concurrently and independently
* Pinger reconciliation ensures consistency between state and active pingers
* Proper mutex protection prevents race conditions on `activePingers` map
* WaitGroup ensures clean shutdown of all goroutines
* Structured logging provides queryable visibility into all operations
* Batch writes significantly improve InfluxDB performance
* Health checks enable production monitoring and automated orchestration
* Comprehensive tests prevent regressions in orchestration logic
