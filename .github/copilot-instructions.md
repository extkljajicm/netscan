# GitHub Copilot Instructions for Project: netscan (Refined)

## Project Goal

To refactor and enhance `netscan`, a Go-based network monitoring service, to be highly performant, resilient, and secure. The service must follow a specific, decoupled workflow:

1.  **Periodic ICMP Discovery:** Scan configured network ranges to find new, active devices.
2.  **Continuous ICMP Monitoring:** For *every* device found, initiate an immediate and continuous high-frequency (e.g., 1-second) ICMP ping to track real-time uptime.
3.  **Dual-Trigger SNMP Scanning:**
    * **Initial:** Perform an SNMP query *immediately* after a device is first discovered.
    * **Scheduled:** Perform a full SNMP scan on *all* known-alive devices at a configurable daily time (e.g., 02:00 AM).
4.  **Resilient Data Persistence:** Write all monitoring (ICMP) and discovery (SNMP) data to InfluxDB, with robust protections against network failures, data corruption, and database overload.

---

## Core Features to Refine

* **Configuration:** Load parameters from `config.yml` with full environment variable support.
    * `icmp_discovery_interval`: How often to scan for *new* devices (e.g., "4h").
    * `ping_interval`: How often to ping *known* devices (e.g., "1s").
    * `snmp_daily_schedule`: What time to run the full SNMP scan (e.g., "02:00").
* **State Management:** A thread-safe, in-memory registry (`StateManager`) is the "single source of truth" for all known devices. It must support adding new devices, updating/enriching existing ones, and pruning stale devices that haven't responded to pings.
* **Decoupled ICMP Discovery:** A dedicated scanner that runs on `icmp_discovery_interval`, sweeps network ranges, and feeds *new* IPs to the `StateManager`.
* **Decoupled SNMP Scanning:** A dedicated, concurrent scanner that can be triggered in two ways:
    1.  On-demand (for a single, newly-discovered IP).
    2.  Scheduled (for *all* IPs currently in the `StateManager`).
* **Continuous Monitoring:** A "reconciliation loop" that ensures *every* device in the `StateManager` has a dedicated, continuous `pinger` goroutine running. This loop must also handle device removal, gracefully stopping the associated goroutine.
* **Resilience & Security (Mandatory):**
    * **Retain All Fixes:** Fully integrate all solutions from `SECURITY_IMPROVEMENTS.md`.
    * **Timeouts:** Apply aggressive `context.WithTimeout` to *all* external calls: ICMP ping, SNMP queries, and InfluxDB writes.
    * **InfluxDB Protection:** Mandate the InfluxDB **health check** on startup, **rate limiting** on writes, and **strict data sanitization/validation** before every write.
    * **Error Handling:** A failed ping or SNMP query must be logged but must *not* crash the pinger or the application.

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

## Refined Architecture & Implementation Plan

### Step 1: Configuration (`internal/config/config.go`)

1.  **Review `Config` Struct:** Ensure it includes:
    * `ICMPDiscoveryInterval time.Duration` (e.g., "4h")
    * `PingInterval time.Duration` (e.g., "1s")
    * `SNMPDailySchedule string` (e.g., "02:00")
    * All resource limits from `SECURITY_IMPROVEMENTS.md` (`max_devices`, etc.).
2.  **Validate:** Add validation for `SNMPDailySchedule` (must be parseable as `HH:MM`).

### Step 2: State Manager (`internal/state/manager.go`)

1.  This is the central hub. It must be fully thread-safe (`sync.RWMutex`).
2.  `Device` struct should store `IP`, `Hostname`, `SysDescr`, `SysObjectID`, and `LastSeen time.Time`.
3.  Implement methods:
    * `AddDevice(ip string) (isNew bool)`: Adds a device. Returns `true` if it was new.
    * `UpdateDeviceSNMP(ip string, snmpData ...)`: Enriches an existing device.
    * `UpdateLastSeen(ip string)`: Called by pingers to keep a device alive.
    * `GetAllIPs() []string`: Returns all IPs for the daily SNMP scan.
    * `PruneStale(olderThan time.Duration)`: Removes devices not seen recently.
    * **Crucial:** Retain the `maxDevices` limit and eviction logic.

### Step 3: InfluxDB Writer (`internal/influx/writer.go`)

1.  **No Regression:** This module must retain *all* hardening from `SECURITY_IMPROVEMENTS.md`.
2.  `NewWriter(...)`: Must perform the **Health Check** and return an error on failure.
3.  `WritePingResult(...)`: Must use a **short timeout** (e.g., 2s) and log errors, not panic.
4.  `WriteDeviceInfo(...)`: Must use a **longer timeout** (e.g., 5s) and perform **data sanitization** on all string fields.
5.  Ensure the client-side **rate limiter** is active.

### Step 4: Scanners (`internal/discovery/scanner.go`)

1.  Refactor this package into two clear, concurrent functions.
2.  `RunICMPSweep(networks []string, workers int) []string`:
    * Uses a worker pool to ping all IPs in the CIDR ranges.
    * **Returns only the IPs that responded.**
    * Must *retain* the CIDR expansion limits (e.g., max /16) from `SECURITY_IMPROVEMENTS.md`.
3.  `RunSNMPScan(ips []string, config *config.SNMP, workers int) []state.Device`:
    * Uses a worker pool to run SNMP queries against the provided list of IPs.
    * Applies timeouts (`config.Timeout`) to each query.
    * Gracefully handles hosts that don't respond to SNMP (logs error, continues).
    * Returns a list of `state.Device` structs with SNMP data filled in.

### Step 5: Continuous Pinger (`internal/monitoring/pinger.go`)

1.  `StartPinger(ctx context.Context, wg *sync.WaitGroup, device state.Device, interval time.Duration, writer *influx.Writer, stateMgr *state.Manager)`:
    * This function runs in its *own goroutine* for *one* device.
    * It loops on a `time.NewTicker(interval)`.
    * Inside the loop:
        1.  Perform a ping (with a short timeout).
        2.  Call `writer.WritePingResult(...)` with the outcome.
        3.  If successful, call `stateMgr.UpdateLastSeen(device.IP)`.
        4.  If ping or write fails, **log the error and continue the loop**. Do not exit.
    * It must listen for `ctx.Done()` to exit gracefully.

### Step 6: Orchestration (`cmd/netscan/main.go`)

This is the most critical refactor. It must manage all concurrent loops and the state.

1.  **Init:**
    * Load and validate config.
    * Init `StateManager`.
    * Init `InfluxWriter` (and **fail fast** if health check fails).
    * Setup signal handling for graceful shutdown (using a main `context.Context`).
    * `activePingers := make(map[string]context.CancelFunc)`
    * `pingersMu sync.Mutex` (**CRITICAL**: Use this mutex for *all* access to `activePingers`).

2.  **Ticker 1: ICMP Discovery Loop** (Runs every `config.ICMPDiscoveryInterval`):
    * `log.Println("Starting ICMP discovery scan...")`
    * `responsiveIPs := discovery.RunICMPSweep(...)`
    * Iterate `responsiveIPs`:
        * `isNew := stateMgr.AddDevice(ip)`
        * If `isNew`:
            * `log.Printf("New device found: %s. Performing initial SNMP scan.", ip)`
            * Trigger an *immediate, non-blocking* scan:
                `go func(newIP string) { ... snmpDevices := discovery.RunSNMPScan([]string{newIP}, ...); ... stateMgr.UpdateDeviceSNMP(newIP, snmpDevices[0]) ... }(ip)`

3.  **Ticker 2: Daily SNMP Scan Loop** (Runs once per day at `config.SNMPDailySchedule`):
    * `log.Println("Starting daily full SNMP scan...")`
    * `allIPs := stateMgr.GetAllIPs()`
    * `snmpDevices := discovery.RunSNMPScan(allIPs, ...)`
    * Iterate `snmpDevices` and update the state:
        * `stateMgr.UpdateDeviceSNMP(...)`
    * `log.Println("Daily SNMP scan complete.")`

4.  **Ticker 3: Pinger Reconciliation Loop** (Runs frequently, e.g., every 5 seconds):
    * This loop ensures the `activePingers` map matches the `StateManager`.
    * `pingersMu.Lock()` (Lock the map for the whole reconciliation).
    * `currentStateIPs := stateMgr.GetAllIPsAsSet()`
    * **Start new pingers:**
        * Iterate `currentStateIPs`:
        * If `ip` is *not* in `activePingers`:
            * `log.Printf("Starting continuous pinger for %s", ip)`
            * Create `pingerCtx, pingerCancel := context.WithCancel(mainCtx)`
            * `activePingers[ip] = pingerCancel`
            * `go monitoring.StartPinger(pingerCtx, ...)`
    * **Stop old pingers:**
        * Iterate `activePingers`:
        * If `ip` is *not* in `currentStateIPs`:
            * `log.Printf("Stopping continuous pinger for stale device %s", ip)`
            * `cancelFunc()`
            * `delete(activePingers, ip)`
    * `pingersMu.Unlock()`

5.  **Ticker 4: State Pruning Loop** (Runs infrequently, e.g., every 1 hour):
    * `log.Println("Pruning stale devices...")`
    * `stateMgr.PruneStale(24 * time.Hour)` (or some other configurable duration).

6.  **Graceful Shutdown:**
    * When the `mainCtx` is canceled (by SIGINT/SIGTERM):
    * Stop all tickers.
    * `pingersMu.Lock()`, iterate `activePingers`, and call `cancelFunc()` for all.
    * `pingersMu.Unlock()`.
    * Wait for `wg.Wait()` on all pingers.
    * Close InfluxDB client.
    * `log.Println("Shutdown complete.")`