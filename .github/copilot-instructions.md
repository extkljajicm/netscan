# GitHub Copilot Instructions for Project: netscan

## Project Goal

`netscan` is a Go-based network monitoring service with a decoupled, multi-ticker architecture. The service follows this workflow:

1.  **Periodic ICMP Discovery:** Scan configured network ranges to find new, active devices.
2.  **Continuous ICMP Monitoring:** For *every* device found, initiate an immediate and continuous high-frequency (e.g., 1-second) ICMP ping to track real-time uptime.
3.  **Dual-Trigger SNMP Scanning:**
    * **Initial:** Perform an SNMP query *immediately* after a device is first discovered.
    * **Scheduled:** Perform a full SNMP scan on *all* known-alive devices at a configurable daily time (e.g., 02:00 AM).
4.  **Resilient Data Persistence:** Write all monitoring (ICMP) and discovery (SNMP) data to InfluxDB, with robust protections against network failures, data corruption, and database overload.

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

**Maintain backward compatibility:**
* Make new fields optional with sensible defaults
* Don't break existing configs when adding new features
* Provide clear error messages for invalid configurations

**Keep config files clean:**
* Remove deprecated fields from example configs
* Reorganize for clarity when adding new features
* Document all required vs optional fields

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

**Make failures obvious:**
* Log when SNMP fails for new devices
* Indicate that failed devices will be retried in next scan
* Don't hide errors but also don't spam logs

### Documentation Best Practices

**Focus on current features:**
* Remove verbose iterative fix details from CHANGELOG
* Keep README concise and focused on capabilities
* Document what the software does, not the development process

**Maintain consistency:**
* Update all docs when making schema or config changes
* Keep README, CHANGELOG, and code comments in sync
* Remove references to deprecated features everywhere

---

## Technology Stack

* **Language**: Go 1.21+
* **Key Libraries**:
    * `gopkg.in/yaml.v3` (Config)
    * `github.com/gosnmp/gosnmp` (SNMP)
    * `github.com/prometheus-community/pro-bing` (ICMP)
    * `github.com/influxdata/influxdb-client-go/v2` (InfluxDB)
    * `sync.RWMutex` (Critical for `StateManager` and `activePingers` map)

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
* Race detection must be clean
* Build must succeed with no warnings

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