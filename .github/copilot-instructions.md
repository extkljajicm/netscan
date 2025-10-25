# GitHub Copilot Instructions for Project: netscan

## Project Overview

`netscan` is a production-grade Go network monitoring service that performs real-time network device discovery and continuous uptime monitoring. Built with Go 1.25.1, the service implements a decoupled, multi-ticker architecture for concurrent network operations. **Target platform: linux-amd64 only.**

The service combines periodic ICMP ping sweeps for device discovery, continuous per-device ICMP monitoring for real-time uptime tracking, and SNMP scanning for device metadata enrichment. All metrics are written to InfluxDB v2 using an optimized batching system that achieves 99% reduction in database requests compared to naive implementations. Deployment options include Docker Compose (recommended) and native systemd installation with capability-based security.

---

## Core Architecture

### Multi-Ticker Event-Driven Design

The application uses five independent, concurrent tickers orchestrated in `cmd/netscan/main.go`, each implementing a specific monitoring workflow:

1. **ICMP Discovery Ticker** (interval: `icmp_discovery_interval`, default 5m)
   - Performs concurrent ICMP ping sweeps across configured networks
   - Uses worker pool pattern with configurable workers (default: 64)
   - Adds responsive IPs to StateManager
   - Triggers immediate SNMP scan for newly discovered devices in background goroutines

2. **Daily SNMP Scan Ticker** (schedule: `snmp_daily_schedule`, HH:MM format)
   - Full SNMP scan of all known devices at scheduled time (e.g., "02:00")
   - Refreshes device metadata (hostname, sysDescr)
   - Uses `createDailySNMPChannel()` for 24-hour scheduling with wraparound handling
   - Disabled if `snmp_daily_schedule` is empty string

3. **Pinger Reconciliation Ticker** (interval: fixed 5 seconds)
   - Ensures every device in StateManager has an active pinger goroutine
   - Starts new pingers for discovered devices
   - Stops pingers for pruned/stale devices
   - Uses `stoppingPingers` map to prevent race conditions during pinger lifecycle transitions
   - Protected by `pingersMu` mutex for thread-safe map access
   - Respects `max_concurrent_pingers` limit

4. **State Pruning Ticker** (interval: fixed 1 hour)
   - Removes devices not seen in last 24 hours via `stateMgr.PruneStale(24 * time.Hour)`
   - Reconciliation loop automatically stops pingers for pruned devices

5. **Health Report Ticker** (interval: `health_report_interval`, default 10s)
   - Writes application health metrics to InfluxDB health bucket
   - Includes device count, active pingers, goroutines, memory (heap + RSS), InfluxDB status

### Concurrency Model

- **Context-Based Cancellation:** Main context cancels all child contexts on shutdown signal (SIGINT, SIGTERM)
- **WaitGroup Tracking:** `pingerWg` tracks all pinger goroutines for graceful shutdown
- **Mutex Protection:** `pingersMu` protects `activePingers` map (IP → context.CancelFunc)
- **Panic Recovery:** All goroutines (workers, pingers, handlers) wrapped with defer panic recovery
- **Non-Blocking Operations:** SNMP scans and state updates use background goroutines to avoid blocking discovery loop

### Initialization Sequence

1. Parse `-config` CLI flag (default: "config.yml")
2. Initialize zerolog structured logging (`logger.Setup(false)`)
3. Load and validate configuration (`config.LoadConfig()`, `config.ValidateConfig()`)
4. Create StateManager with LRU eviction (`state.NewManager(cfg.MaxDevices)`)
5. Create InfluxDB writer with batching (`influx.NewWriter()`)
6. Perform InfluxDB health check (fail-fast with `log.Fatal()` on error)
7. Initialize `activePingers` map and `stoppingPingers` map
8. Initialize `pingersMu` mutex and `pingerWg` WaitGroup
9. Start health check HTTP server with accurate pinger count callback
10. Setup signal handling for graceful shutdown
11. Create main context with cancel function
12. Run initial ICMP discovery scan before entering main event loop

### Graceful Shutdown Sequence

1. Signal received (SIGINT/SIGTERM) triggers `stop()` to cancel main context
2. Stop all five tickers
3. Acquire `pingersMu` lock
4. Iterate `activePingers` map and call all cancel functions
5. Release `pingersMu` lock
6. Call `pingerWg.Wait()` to wait for all pinger goroutines to exit
7. Call `writer.Close()` to flush remaining batched points from both WriteAPIs
8. Log "Shutdown complete" and return from main

---

## Deployment Model

### Platform Constraint

**Target platform: linux-amd64 only.** All documentation, builds, and deployment instructions focus exclusively on 64-bit x86 Linux systems. Multi-architecture support (ARM, etc.) is deferred to future work.

### Docker Deployment (Primary - Recommended)

**Multi-Stage Dockerfile:**
- **Builder Stage:** `golang:1.25-alpine` with CGO_ENABLED=0, GOOS=linux, GOARCH=amd64
- **Binary Stripping:** `-ldflags="-w -s"` for minimal size (~15MB final image)
- **Runtime Stage:** `alpine:latest` with ca-certificates, libcap, wget
- **Capabilities:** Uses `setcap cap_net_raw+ep` on binary
- **Security Note:** Container **must run as root** (non-negotiable for ICMP raw socket access in Linux containers)

**Docker Compose Stack (docker-compose.yml):**
- **Services:** netscan + InfluxDB v2.7
- **Network Mode:** `network_mode: host` for direct network access to scan targets
- **Capabilities:** `cap_add: NET_RAW` for ICMP
- **Log Rotation:** 10MB max per file, 3 files retained (~30MB total)
- **Configuration Mount:** `./config.yml:/app/config.yml:ro` (read-only)
- **Environment Variables:** `.env` file or inline defaults for credential expansion
- **Health Checks:** HEALTHCHECK using `/health/live` endpoint (30s interval, 3s timeout, 3 retries, 40s start period)
- **Dual-Bucket InfluxDB:** Automatic creation of "netscan" and "health" buckets via `init-influxdb.sh`
- **Deployment Validation:** `docker-verify.sh` script in CI/CD workflow

### Native systemd Deployment (Alternative - Maximum Security)

**Security Model (deploy.sh):**
- **Dedicated User:** Creates `netscan` system user with `/bin/false` shell (no login)
- **Capability-Based Access:** `setcap cap_net_raw+ep` on binary (no root execution required)
- **Secure Credentials:** `/opt/netscan/.env` file with mode 600 (owner-only read/write)
- **Installation Location:** `/opt/netscan/` with binary, config, and .env file

**Systemd Service Hardening:**
- **PrivateTmp:** Isolated /tmp directory
- **ProtectSystem=strict:** Read-only root filesystem
- **NoNewPrivileges:** Prevents privilege escalation
- **Environment File:** `EnvironmentFile=/opt/netscan/.env` for secure credential loading

**Deployment Scripts:**
- `deploy.sh`: Automated installation with user creation, binary compilation, capability setting, systemd service registration
- `undeploy.sh`: Clean removal of service, user, and files

---

## Core Components (Features)

### Configuration System (`internal/config/config.go`)

**YAML Configuration Loading:**
- **Primary File:** `config.yml` with `-config` flag support for custom path
- **Environment Variable Expansion:** All `${VAR_NAME}` placeholders expanded via `os.ExpandEnv()`
- **Backward Compatibility:** `discovery_interval` field optional (deprecated, defaults to 4h)
- **Validation on Startup:** `ValidateConfig()` performs security and sanity checks with clear error messages

**Configuration Parameters with Defaults:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `discovery_interval` | duration | 4h | (Deprecated) Legacy interval field |
| `icmp_discovery_interval` | duration | (required) | How often to scan for new devices |
| `ping_interval` | duration | (required) | How often to ping known devices |
| `ping_timeout` | duration | (required) | Timeout for individual ping operations |
| `icmp_workers` | int | 64 | Concurrent ICMP discovery workers |
| `snmp_workers` | int | 32 | Concurrent SNMP scanning workers |
| `networks` | []string | (required) | List of CIDR ranges to scan |
| `snmp.community` | string | (required) | SNMPv2c community string |
| `snmp.port` | int | (required) | SNMP port (typically 161) |
| `snmp.timeout` | duration | 5s | SNMP query timeout |
| `snmp.retries` | int | (required) | SNMP retry count |
| `snmp_daily_schedule` | string | "" | Daily SNMP scan time (HH:MM), empty disables |
| `health_check_port` | int | 8080 | HTTP health endpoint port |
| `health_report_interval` | duration | 10s | Interval for writing health metrics |
| `max_concurrent_pingers` | int | 20000 | Maximum active pinger goroutines |
| `max_devices` | int | 20000 | Maximum devices (LRU eviction) |
| `min_scan_interval` | duration | 1m | Minimum discovery scan interval |
| `memory_limit_mb` | int | 16384 | Memory usage warning threshold |
| `influxdb.url` | string | (required) | InfluxDB server URL |
| `influxdb.token` | string | (required) | InfluxDB authentication token |
| `influxdb.org` | string | (required) | InfluxDB organization |
| `influxdb.bucket` | string | (required) | InfluxDB primary bucket |
| `influxdb.health_bucket` | string | "health" | InfluxDB health metrics bucket |
| `influxdb.batch_size` | int | 5000 | Points per batch |
| `influxdb.flush_interval` | duration | 5s | Maximum time before flush |

**Validation Features:**
- CIDR notation validation with dangerous network checks (loopback, multicast, link-local)
- Worker count bounds (ICMP: 1-2000, SNMP: 1-1000)
- Interval minimums (discovery ≥ 1m, ping ≥ 1s)
- Time format validation (HH:MM for daily schedule)
- SNMP community string sanitization and weak password detection
- URL validation with scheme checking (http/https)
- Resource limit bounds validation

### State Management (`internal/state/manager.go`)

**Thread-Safe Device Registry:**
- **Single Source of Truth:** Central `Manager` with `sync.RWMutex` protecting all operations
- **Device Struct:**
  ```go
  type Device struct {
      IP       string    // IPv4 address
      Hostname string    // From SNMP or defaults to IP
      SysDescr string    // SNMP sysDescr MIB-II value
      LastSeen time.Time // Timestamp of last successful ping
  }
  ```

**Public Methods:**
- `NewManager(maxDevices int) *Manager`: Factory constructor with device limit (default: 10000)
- `Add(device Device)`: Idempotent device insertion with LRU eviction
- `AddDevice(ip string) bool`: Add device by IP only, returns true if new
- `Get(ip string) (*Device, bool)`: Retrieve device by IP
- `GetAll() []Device`: Return copy of all devices (slice, not map)
- `GetAllIPs() []string`: Return slice of all device IPs
- `UpdateLastSeen(ip string)`: Refresh timestamp (called by pingers on successful ping)
- `UpdateDeviceSNMP(ip, hostname, sysDescr string)`: Enrich device with SNMP metadata
- `Prune(olderThan time.Duration) []Device`: Remove stale devices, returns removed list
- `PruneStale(olderThan time.Duration) []Device`: Alias for Prune (clearer naming)
- `Count() int`: Return current device count

**LRU Eviction:**
- When `max_devices` limit reached, automatically removes oldest device (by LastSeen timestamp) before adding new one
- Eviction occurs in both `Add()` and `AddDevice()` methods
- Thread-safe with write lock held during eviction

### ICMP Discovery (`internal/discovery/scanner.go`)

**Function Signature:**
```go
func RunICMPSweep(networks []string, workers int) []string
```

**Worker Pool Pattern:**
- Concurrent ping sweep with configurable workers (default: 64)
- Buffered channels (capacity: 256) for jobs and results
- Producer goroutine expands CIDR ranges and streams IPs to jobs channel
- Worker goroutines consume IPs, perform pings, send responsive IPs to results

**Implementation Details:**
- **CIDR Expansion:** `streamIPsFromCIDR()` streams IPs without intermediate array allocation
- **Ping Configuration:** 1 second timeout, single ICMP echo request per IP
- **Privileged Mode:** `SetPrivileged(true)` for raw ICMP sockets
- **Returns:** Only IP addresses that responded to ICMP echo request
- **Panic Recovery:** All worker and producer goroutines protected
- **Safety Limits:** Networks larger than /16 (65K hosts) logged with warning

### SNMP Scanning (`internal/discovery/scanner.go`)

**Function Signature:**
```go
func RunSNMPScan(ips []string, snmpConfig *config.SNMPConfig, workers int) []state.Device
```

**Worker Pool Pattern:**
- Concurrent SNMP queries with configurable workers (default: 32)
- Buffered channels (capacity: 256) for jobs and results
- Timeout: Configurable via `config.SNMP.Timeout` (default: 5s)
- Returns: Slice of `state.Device` structs with SNMP data populated
- Graceful Failure: Devices that don't respond to SNMP are logged and skipped (not errors)

**SNMP Robustness Features:**
- **`snmpGetWithFallback()` Function:**
  - Tries SNMP Get first (efficient for .0 instances)
  - Falls back to GetNext if NoSuchInstance/NoSuchObject error
  - Validates returned OID is under requested base OID
  - Prevents incorrect fallback data
- **`validateSNMPString()` Type Handling:**
  - Handles both `string` and `[]byte` (OctetString) types
  - Converts byte arrays to strings for ASCII/UTF-8 data
  - Sanitizes control characters (removes null bytes, replaces newlines with spaces)
  - Truncates to 1024 characters max
  - Eliminates "invalid type: expected string, got []uint8" errors

**Queried OIDs:**
- `1.3.6.1.2.1.1.5.0` (sysName): Device hostname
- `1.3.6.1.2.1.1.1.0` (sysDescr): System description
- Both use `snmpGetWithFallback()` for maximum device compatibility

### Continuous Monitoring (`internal/monitoring/pinger.go`)

**Function Signature:**
```go
func StartPinger(ctx context.Context, wg *sync.WaitGroup, device state.Device, 
                 interval time.Duration, timeout time.Duration, 
                 writer PingWriter, stateMgr StateManager)
```

**Lifecycle:**
- Runs in dedicated goroutine per device
- Continues until context cancelled (graceful shutdown)
- Uses `time.NewTicker(interval)` for consistent ping frequency (default: 2s)

**Operation:**
- **IP Validation:** Comprehensive security checks before each ping:
  - Format validation via `net.ParseIP()`
  - Rejects loopback, multicast, link-local, and unspecified addresses
  - Prevents dangerous network scanning
- **Timeout:** Configurable per ping operation (default: 3s)
- **Success Criteria:** `len(stats.Rtts) > 0 && stats.AvgRtt > 0` (more reliable than PacketsRecv)
- **State Updates:** Calls `stateMgr.UpdateLastSeen(ip)` on successful ping
- **Metrics Writing:** Calls `writer.WritePingResult(ip, rtt, success)` for all ping attempts
- **Error Handling:** Logs all errors with structured context, continues loop (does not exit on failure)
- **Panic Recovery:** Protected with panic recovery at function entry
- **Graceful Shutdown:** Listens for `ctx.Done()` and exits cleanly, decrements WaitGroup

**Interface Design:**
```go
type PingWriter interface {
    WritePingResult(ip string, rtt time.Duration, successful bool) error
    WriteDeviceInfo(ip, hostname, sysDescr string) error
}

type StateManager interface {
    UpdateLastSeen(ip string)
}
```
Enables easy mocking for unit tests.

### InfluxDB Writer (`internal/influx/writer.go`)

**High-Performance Batch System:**
- **Architecture:** Lock-free channel-based batching with background flusher goroutine
- **Dual-Bucket:** Separate WriteAPI instances for primary metrics and health metrics
- **Constructor:** `NewWriter(url, token, org, bucket, healthBucket string, batchSize int, flushInterval time.Duration) *Writer`
- **Health Check:** `HealthCheck() error` - Performs connectivity test via client.Health() API
- **Metrics Tracking:** Atomic counters (`atomic.Uint64`) for successful and failed batch writes

**Batching Architecture:**
- **Channel-Based Queue:** Points queued to buffered channel (capacity: `batchSize * 2`)
- **Background Flusher Goroutine:** Accumulates points and flushes on two triggers:
  1. Batch full (reached `batchSize` points)
  2. Timer fires (after `flushInterval` duration)
- **Non-Blocking Writes:** `WritePingResult()` and `WriteDeviceInfo()` immediately return after queuing point
- **Graceful Shutdown:** `Close()` method:
  1. Cancels context to stop background flusher
  2. Background flusher drains remaining points from channel
  3. Performs final flush on both WriteAPIs
  4. Closes InfluxDB client
- **Error Monitoring:** Separate goroutine monitors both WriteAPI error channels (obtained once during init) and logs failures with bucket context
- **Retry Logic:** `flushWithRetry()` attempts up to 3 retries with exponential backoff (1s, 2s, 4s)

**Write Methods:**

1. **WritePingResult(ip string, rtt time.Duration, successful bool) error**
   - Measurement: "ping"
   - Tags: "ip"
   - Fields: "rtt_ms" (float64), "success" (bool)
   - Queued to batch channel (non-blocking)

2. **WriteDeviceInfo(ip, hostname, description string) error**
   - Measurement: "device_info"
   - Tags: "ip"
   - Fields: "hostname" (string), "snmp_description" (string)
   - All strings sanitized via `sanitizeInfluxString()`
   - Queued to batch channel (non-blocking)

3. **WriteHealthMetrics(deviceCount, pingerCount, goroutines, memMB, rssMB int, influxOK bool, influxSuccess, influxFailed uint64)**
   - Measurement: "health_metrics"
   - Tags: none
   - Fields: "device_count", "active_pingers", "goroutines", "memory_mb", "rss_mb", "influxdb_ok", "influxdb_successful_batches", "influxdb_failed_batches"
   - Note: memory_mb is Go heap (runtime.MemStats.Alloc), rss_mb is OS RSS (Linux /proc/self/status VmRSS)
   - Written directly to healthWriteAPI (uses InfluxDB client's internal batching)

**Data Sanitization (`sanitizeInfluxString`):**
- Limits string length to 500 characters (adds "..." if truncated)
- Removes control characters except tab (\t) and newline (\n)
- Allows printable ASCII (32-126)
- Trims whitespace

**IP Address Validation (`validateIPAddress`):**
- Empty check
- Format validation via `net.ParseIP()`
- Rejects loopback, multicast, link-local, unspecified addresses

**Metrics Methods:**
- `GetSuccessfulBatches() uint64`: Returns atomic counter of successful batch writes
- `GetFailedBatches() uint64`: Returns atomic counter of failed batch writes

**Performance:** 99% reduction in InfluxDB HTTP requests for deployments with 100+ devices (batching 5000 points vs individual writes).

### Health Check Server (`cmd/netscan/health.go`)

**HTTP Server:**
- Non-blocking server on configurable port (default: 8080)
- Started via `healthServer.Start()` with goroutine for `http.ListenAndServe()`

**Three Endpoints:**

1. **`/health`** - Detailed JSON status with full metrics
   ```json
   {
     "status": "healthy|degraded|unhealthy",
     "version": "1.0.0",
     "uptime": "1h23m45s",
     "device_count": 142,
     "active_pingers": 142,
     "influxdb_ok": true,
     "influxdb_successful": 12543,
     "influxdb_failed": 0,
     "goroutines": 156,
     "memory_mb": 34,
     "rss_mb": 128,
     "timestamp": "2025-10-24T13:00:00Z"
   }
   ```

2. **`/health/ready`** - Readiness probe
   - Returns 200 if InfluxDB OK (connectivity check passes)
   - Returns 503 if InfluxDB unavailable
   - Body: "READY" or "NOT READY: InfluxDB unavailable"

3. **`/health/live`** - Liveness probe
   - Returns 200 if application responding
   - Body: "ALIVE"

**Health Response Fields:**

| Field | Type | Description |
|-------|------|-------------|
| `status` | string | "healthy", "degraded", or "unhealthy" based on InfluxDB connectivity |
| `version` | string | Application version (currently hardcoded "1.0.0") |
| `uptime` | string | Human-readable uptime since startup |
| `device_count` | int | Number of devices in StateManager |
| `active_pingers` | int | Accurate count via mutex-protected callback to len(activePingers) |
| `influxdb_ok` | bool | Result of HealthCheck() call |
| `influxdb_successful` | uint64 | Successful batch writes counter |
| `influxdb_failed` | uint64 | Failed batch writes counter |
| `goroutines` | int | `runtime.NumGoroutine()` count |
| `memory_mb` | uint64 | Go heap allocation (runtime.MemStats.Alloc / 1024 / 1024) |
| `rss_mb` | uint64 | OS-level resident set size (Linux /proc/self/status VmRSS in MB) |
| `timestamp` | time.Time | Current timestamp |

**Note on Memory Metrics:**
- `memory_mb`: Go heap allocation (runtime.MemStats.Alloc) - only Go-managed memory
- `rss_mb`: OS-level resident set size (Linux /proc/self/status VmRSS) - total memory pages in RAM including Go heap, stack, C libraries, and OS overhead
- RSS parsing via `getRSSMB()` function: reads /proc/self/status, parses VmRSS line, converts kB to MB

**Docker Integration:**
- HEALTHCHECK directive in Dockerfile: `wget --spider http://localhost:8080/health/live`
- Docker Compose healthcheck: 30s interval, 3s timeout, 3 retries, 40s start period

**Kubernetes Integration:**
- Compatible with livenessProbe (use `/health/live`)
- Compatible with readinessProbe (use `/health/ready`)
- Enables automated pod restart on failure and traffic routing control

### Structured Logging (`internal/logger/logger.go`)

**Library:** zerolog v1.34.0 for zero-allocation performance

**Setup Function:**
```go
func Setup(debugMode bool)
```
- Initializes global logger with service name "netscan"
- Adds timestamp to all logs
- Sets log level: Debug if debugMode=true, Info otherwise
- Development mode: Colored console output when `ENVIRONMENT=development` env var set
- Production mode: Machine-parseable JSON to stdout

**Log Levels:**
- **Fatal:** Configuration errors, startup failures (exits process with `log.Fatal()`)
- **Error:** Failed operations requiring investigation (ping failures, SNMP errors, InfluxDB write errors)
- **Warn:** Resource limits, configuration warnings, non-critical issues
- **Info:** All major operations (startup, scans, discovery, shutdown, summaries)
- **Debug:** Verbose details (pinger lifecycle, individual ping results, SNMP query details)

**Usage Pattern:**
```go
log.Info().
    Str("ip", "192.168.1.1").
    Dur("rtt", rtt).
    Msg("Ping successful")
```

**Common Context Fields:**
- `ip`: Device IP address
- `device_count`: Number of devices
- `rtt`: Round-trip time as duration
- `error`: Error value via `.Err(err)`
- `duration`: Operation duration
- `networks`: Network ranges being scanned

---

## Technology Stack

**Language:** Go 1.25.1
- Module: `github.com/kljama/netscan`
- Specified in go.mod: `go 1.25.1`

**Platform:** linux-amd64 (exclusively)
- Build: `GOOS=linux GOARCH=amd64`
- No multi-architecture support currently

**Primary Dependencies (from go.mod):**
- `gopkg.in/yaml.v3 v3.0.1` - YAML configuration parsing
- `github.com/gosnmp/gosnmp v1.42.1` - SNMPv2c protocol implementation
- `github.com/prometheus-community/pro-bing v0.7.0` - ICMP ping implementation with raw socket support
- `github.com/influxdata/influxdb-client-go/v2 v2.14.0` - InfluxDB v2 client with WriteAPI
- `github.com/rs/zerolog v1.34.0` - Zero-allocation structured logging (JSON and console)

**Standard Library Usage:**
- `sync.RWMutex` - Critical for StateManager and activePingers map protection
- `sync.WaitGroup` - Pinger goroutine lifecycle tracking
- `context.Context` - Cancellation propagation and graceful shutdown
- `time.Ticker` - Five independent event loops
- `net` - IP address validation and parsing
- `flag` - CLI argument parsing (`-config` flag)

**Infrastructure:**
- **Docker:** Multi-stage Dockerfile with `golang:1.25-alpine` builder, `alpine:latest` runtime
- **InfluxDB:** v2.7 container for time-series metrics storage
- **Alpine Linux:** Base image for minimal attack surface (~15MB final image)

**Build Configuration:**
- `CGO_ENABLED=0` - Static binary (no C dependencies)
- `-ldflags="-w -s"` - Stripped binary (smaller size)
- `GOOS=linux GOARCH=amd64` - Target platform specification

**Testing:**
- `go test` - Standard testing framework
- `go test -race` - Race condition detection (all tests pass)
- `go test -cover` - Code coverage reporting

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
* **Mandate: All SNMP string processing MUST handle `[]byte` (OctetString).** Use a helper function (like `validateSNMPString()`) to check for both `string` and `[]byte` types and convert as needed to prevent "invalid type" errors.

### Logging & Data Mandates

* **Mandate: All new components MUST include diagnostic logging.** At a minimum, log:
    * Configuration values being used (e.g., "Scanning networks: %v").
    * Entry/exit of major operations.
    * Specific error details with context (e.g., "Ping failed for %s: %v"). Do not silently fail.
* **Mandate: Summary logs MUST report both success and failure counts.** (e.g., "Enriched X devices (failed: Y)").
* **Mandate: The InfluxDB schema MUST remain simple.** Do not add fields that are not actively used for monitoring or queries (e.g., `sysObjectID`). The primary schema is `device_info` with `IP`, `hostname`, and `snmp_description`.
* **Mandate: Log all successful writes to InfluxDB.** Include the device identifier (IP/hostname) in the log message for debugging and confirmation.
* **Mandate: Documentation Parity.** Code changes are **not complete** until all user-facing documentation (`MANUAL.md` and `README.md`) is updated to reflect the change. This applies to *all* changes, including bug fixes, new features, and, critically, **any change to a default value**. Specifically, you must verify that:
    * `config.yml.example` matches the new defaults.
    * The "Configuration Reference" in `MANUAL.md` is updated with any new parameters or modified defaults.
    * Any performance tuning guidance or tables (like the "Worker Count Guidelines") are adjusted to reflect new recommendations.
    * Documentation is a core part of the commit, not a future task.

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

### Testing Mandates

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
    * Default batch size: 5000 points
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

## Common Maintenance Procedures

- **To start a fresh deployment and delete all InfluxDB data:** Use the command `docker-compose down -v`. This stops the containers and removes the associated `influxdb-data` volume.

- **To build and run the latest code changes:** Use the command `docker-compose up -d --build`. This forces a rebuild of the `netscan` Docker image from the local source code before starting the container.

- **To reclaim unused Docker disk space:** Use the command `docker system prune`. This cleans up dangling images, stopped containers, and unused build cache.
