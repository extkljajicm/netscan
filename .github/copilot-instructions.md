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

## Core Features (Implemented)

* **Configuration:** Load parameters from `config.yml` with full environment variable support.
    * `icmp_discovery_interval`: How often to scan for *new* devices (e.g., "5m").
    * `ping_interval`: How often to ping *known* devices (e.g., "1s").
    * `snmp_daily_schedule`: What time to run the full SNMP scan (e.g., "02:00").
    * `discovery_interval`: Optional for backward compatibility (defaults to 4h if omitted).
* **State Management:** A thread-safe, in-memory registry (`StateManager`) is the "single source of truth" for all known devices. It supports adding new devices, updating/enriching existing ones, and pruning stale devices that haven't responded to pings.
* **Decoupled ICMP Discovery:** A dedicated scanner that runs on `icmp_discovery_interval`, sweeps network ranges, and feeds *new* IPs to the `StateManager`.
* **Decoupled SNMP Scanning:** A dedicated, concurrent scanner that can be triggered in two ways:
    1.  On-demand (for a single, newly-discovered IP).
    2.  Scheduled (for *all* IPs currently in the `StateManager`).
* **Continuous Monitoring:** A "reconciliation loop" that ensures *every* device in the `StateManager` has a dedicated, continuous `pinger` goroutine running. This loop also handles device removal, gracefully stopping the associated goroutine.
* **Resilience & Security (Mandatory):**
    * **All Fixes Integrated:** All solutions from `SECURITY_IMPROVEMENTS.md` are implemented.
    * **Timeouts:** Aggressive `context.WithTimeout` applied to *all* external calls: ICMP ping, SNMP queries, and InfluxDB writes.
    * **InfluxDB Protection:** InfluxDB **health check** on startup, **rate limiting** on writes, and **strict data sanitization/validation** before every write.
    * **Error Handling:** A failed ping or SNMP query is logged but does *not* crash the pinger or the application.

---

## Lessons Learned from Production Implementation

### Docker Deployment Lessons

**ICMP permissions in containers:**
* Non-root users cannot create raw ICMP sockets even with CAP_NET_RAW in containerized environments
* This is a Linux kernel security limitation, not a Docker configuration issue
* Solution: Container must run as root for ICMP ping functionality
* Trade-off accepted: Root user necessary but container remains isolated via Docker namespaces

**Configuration management:**
* Use environment variables in docker-compose.yml with `${VAR:-default}` syntax
* Supports optional .env file for production credentials (not committed to git)
* Config placeholders like `${INFLUXDB_TOKEN}` automatically expanded by Go's `os.ExpandEnv()`
* No manual sed replacements needed - just `cp config.yml.example config.yml`

**Diagnostic logging is essential:**
* Add logging to show which networks are being scanned: `log.Printf("Scanning networks: %v", cfg.Networks)`
* Log specific error details for ICMP failures: `log.Printf("Ping failed for %s: %v", ip, err)`
* Helps identify whether issue is config reading, permissions, or network access
* Critical for troubleshooting containerized deployments

**Multi-stage builds:**
* Stage 1: Build with `golang:1.25-alpine` (matches go.mod requirement)
* Stage 2: Runtime with minimal `alpine:latest` (~15MB final image)
* Use `GOOS=linux GOARCH=amd64` for consistent builds
* Apply binary optimizations: `-ldflags="-w -s"` strips debug info

### SNMP Best Practices

**Always use fallback mechanisms:**
* SNMP Get can fail with NoSuchInstance errors on devices that support SNMP but don't have .0 OID instances
* Implement `snmpGetWithFallback()`: Try Get first (efficient), fall back to GetNext if needed
* Validate OID prefixes when using GetNext to ensure results are under the requested base OID

**Handle diverse data types:**
* gosnmp library returns OctetString values as `[]byte`, not `string`
* Always check for both `string` and `[]byte` types when processing SNMP responses
* Convert byte arrays to strings using `string(v)` for ASCII/UTF-8 encoded values

### Configuration Best Practices

**Environment variable support:**
* Docker deployment: Environment variables from docker-compose.yml (or optional .env file)
* Native deployment: Environment variables from .env file (required)
* Always document which approach applies to which deployment method

**Maintain backward compatibility:**
* Make new fields optional with sensible defaults
* Don't break existing configs when adding new features
* Provide clear error messages for invalid configurations

**Network configuration warnings:**
* Example network ranges (192.168.0.0/24, etc.) are real but may not match user's network
* Add prominent warnings in config.yml.example that users must update these
* Explain how to find actual network range (ip addr, ifconfig)

### InfluxDB Schema Best Practices

**Keep it simple:**
* Remove redundant fields (e.g., don't store hostname twice as hostname and snmp_name)
* Remove fields that aren't used for monitoring or queries (e.g., sysObjectID)
* Simplified schema: device_info measurement with only IP, hostname, and snmp_description

**Write confirmation:**
* Log each successful write to InfluxDB for debugging
* Include device identifier in logs (IP and hostname)
* Makes it immediately clear when data is/isn't being persisted

### Logging Best Practices

**Be specific about counts:**
* Show both success and failure counts: `"enriched X devices (failed: Y)"`
* Log each operation result individually for debugging
* Provide summary logs after operations complete

**Diagnostic logging for troubleshooting:**
* Log networks being scanned to verify config reading
* Log specific error messages (don't silently fail)
* Add context: which phase (discovery, monitoring, SNMP), which device
* Makes troubleshooting issues much easier, especially in Docker

### Documentation Best Practices

**Separate deployment methods clearly:**
* README.md: Docker deployment (primary)
* README_NATIVE.md: Native installation (alternative for maximum security)
* Don't mix deployment instructions - keeps docs focused and clear

**Security implications must be explained:**
* Document why Docker container runs as root (ICMP raw socket requirement)
* Explain security measures despite root user (isolation, minimal capabilities)
* Provide comparison table: Docker (root) vs Native (service user)
* Guide users to native deployment if maximum security is priority

**Troubleshooting sections are critical:**
* Include diagnostic commands users can run
* Explain common failure modes and how to identify them
* Provide step-by-step verification procedures
* Docker troubleshooting especially important due to permission complexities

---

## Technology Stack

* **Language**: Go 1.25+ (updated from 1.21 - ensure Dockerfile uses golang:1.25-alpine)
* **Key Libraries**:
    * `gopkg.in/yaml.v3` (Config)
    * `github.com/gosnmp/gosnmp` (SNMP)
    * `github.com/prometheus-community/pro-bing` (ICMP)
    * `github.com/influxdata/influxdb-client-go/v2` (InfluxDB)
    * `sync.RWMutex` (Critical for `StateManager` and `activePingers` map)
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
* Race detection must be clean: `go test -race ./...`
* Build must succeed with no warnings
* Docker Compose workflow validates full stack deployment
* Workflow creates config.yml from template and runs `docker compose up`

---

## Architecture & Implementation Details

### Configuration (`internal/config/config.go`)

**Implemented:**
* `Config` struct includes:
    * `ICMPDiscoveryInterval time.Duration` (e.g., "5m")
    * `PingInterval time.Duration` (e.g., "1s")
    * `SNMPDailySchedule string` (e.g., "02:00")
    * All resource limits from `SECURITY_IMPROVEMENTS.md` (`max_devices`, etc.)
    * `DiscoveryInterval time.Duration` (optional for backward compatibility, defaults to 4h)
* Validation for `SNMPDailySchedule` (must be parseable as `HH:MM`, range 00:00 to 23:59)
* Better error messages for invalid duration fields

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
    * `PruneStale(olderThan time.Duration)`: Removes devices not seen recently.
    * `Add(ip string)`: Legacy method maintained for compatibility.
    * `Prune(olderThan time.Duration)`: Alias for PruneStale.
* `maxDevices` limit and eviction logic fully implemented

### InfluxDB Writer (`internal/influx/writer.go`)

**Implemented:**
* All hardening from `SECURITY_IMPROVEMENTS.md` retained
* `NewWriter(...)`: Performs **Health Check** and returns an error on failure
* `WritePingResult(...)`: Uses **short timeout** (2s), logs errors, never panics
* `WriteDeviceInfo(ip, hostname, description string)`: 
    * Uses **longer timeout** (5s)
    * Performs **data sanitization** on all string fields
    * Simplified signature (removed redundant snmp_name and snmp_sysid fields)
* Client-side **rate limiter** is active
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

### Continuous Pinger (`internal/monitoring/pinger.go`)

**Implemented:**
* `StartPinger(ctx context.Context, wg *sync.WaitGroup, device state.Device, interval time.Duration, writer PingWriter, stateMgr StateManager)`:
    * Runs in its *own goroutine* for *one* device
    * Loops on a `time.NewTicker(interval)`
    * Inside the loop:
        1.  Perform a ping (with a short timeout)
        2.  Call `writer.WritePingResult(...)` with the outcome
        3.  If successful, call `stateMgr.UpdateLastSeen(device.IP)`
        4.  If ping or write fails, **log the error and continue the loop** (does not exit)
    * Listens for `ctx.Done()` to exit gracefully
    * Uses interface types (`PingWriter`, `StateManager`) for better testability
    * Properly tracks shutdown with WaitGroup

### Orchestration (`cmd/netscan/main.go`)

**Implemented - Multi-Ticker Architecture:**

**1. Initialization:**
* Load and validate config (with `-config` flag support)
* Init `StateManager`
* Init `InfluxWriter` with **fail fast** if health check fails
* Setup signal handling for graceful shutdown (using a main `context.Context`)
* `activePingers := make(map[string]context.CancelFunc)`
* `pingersMu sync.Mutex` (**CRITICAL**: Used for *all* access to `activePingers`)
* `var pingerWg sync.WaitGroup` for tracking all pinger goroutines

**2. Ticker 1: ICMP Discovery Loop** (Runs every `config.ICMPDiscoveryInterval`, e.g., 5m):
* Logs: `"Starting ICMP discovery scan..."`
* `responsiveIPs := discovery.RunICMPSweep(...)`
* For each `responsiveIP`:
    * `isNew := stateMgr.AddDevice(ip)`
    * If `isNew`:
        * Logs: `"New device found: %s. Performing initial SNMP scan."`
        * Triggers *immediate, non-blocking* scan in goroutine
        * SNMP scan results update StateManager and write to InfluxDB
        * Logs success or failure for debugging

**3. Ticker 2: Daily SNMP Scan Loop** (Runs at `config.SNMPDailySchedule`, e.g., "02:00"):
* Calculates next run time using time parsing
* Logs: `"Starting daily full SNMP scan..."`
* `allIPs := stateMgr.GetAllIPs()`
* `snmpDevices := discovery.RunSNMPScan(allIPs, ...)`
* Updates StateManager with SNMP data
* Writes device info to InfluxDB
* Logs: `"Daily SNMP scan complete."`
* **Enhanced logging:** Shows success/failure counts, confirms InfluxDB writes

**4. Ticker 3: Pinger Reconciliation Loop** (Runs every 5 seconds):
* Ensures `activePingers` map matches `StateManager`
* `pingersMu.Lock()` (Lock for entire reconciliation)
* `currentStateIPs := stateMgr.GetAllIPs()` converted to set
* **Start new pingers:**
    * For each IP in state not in `activePingers`:
        * Logs: `"Starting continuous pinger for %s"`
        * Creates `pingerCtx, pingerCancel := context.WithCancel(mainCtx)`
        * `activePingers[ip] = pingerCancel`
        * `pingerWg.Add(1)`
        * `go monitoring.StartPinger(pingerCtx, &pingerWg, ...)`
* **Stop old pingers:**
    * For each IP in `activePingers` not in state:
        * Logs: `"Stopping continuous pinger for stale device %s"`
        * Calls `cancelFunc()`
        * `delete(activePingers, ip)`
* `pingersMu.Unlock()`

**5. Ticker 4: State Pruning Loop** (Runs every 1 hour):
* Logs: `"Pruning stale devices..."`
* `stateMgr.PruneStale(24 * time.Hour)`

**6. Graceful Shutdown:**
* When `mainCtx` is canceled (by SIGINT/SIGTERM):
    * Stop all tickers
    * `pingersMu.Lock()`, iterate `activePingers`, call `cancelFunc()` for all
    * `pingersMu.Unlock()`
    * `pingerWg.Wait()` to wait for all pingers to exit
    * Close InfluxDB client
    * Logs: `"Shutdown complete."`

**Key Implementation Notes:**
* All four tickers run concurrently and independently
* Pinger reconciliation ensures consistency between state and active pingers
* Proper mutex protection prevents race conditions on `activePingers` map
* WaitGroup ensures clean shutdown of all goroutines
* Enhanced logging provides visibility into all operations