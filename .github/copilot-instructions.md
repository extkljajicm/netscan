# GitHub Copilot Instructions for Project: netscan

## Project Overview

`netscan` is a production-grade Go network monitoring service that performs automated network device discovery and continuous uptime monitoring. The service operates through a multi-ticker event-driven architecture that concurrently executes five independent monitoring workflows: periodic ICMP ping sweeps for device discovery, scheduled SNMP scans for device metadata enrichment, continuous per-device ICMP monitoring with rate limiting, automatic pinger lifecycle management (reconciliation), and state pruning for stale devices. Additionally, it continuously reports health metrics to enable operational observability.

All discovered devices are stored in a central StateManager (the single source of truth), and all metrics are written to InfluxDB v2 using an optimized batching system. The service implements comprehensive concurrency safety through mutexes, context-based cancellation, WaitGroups, and panic recovery throughout all goroutines. Deployment is supported via Docker Compose with InfluxDB or native systemd installation with capability-based security.

---

## Core Architecture

### Multi-Ticker Event-Driven Design

The application uses five independent, concurrent tickers orchestrated in `cmd/netscan/main.go`, each implementing a specific monitoring workflow. All tickers run in a single select statement within the main event loop and are controlled by a shared context for graceful shutdown.

1. **ICMP Discovery Ticker** (`icmpDiscoveryTicker`)
   - **Interval:** Configurable via `cfg.IcmpDiscoveryInterval`
   - **Purpose:** Periodically scans configured network ranges to discover responsive devices
   - **Operation:** 
     - Calls `discovery.RunICMPSweep()` with context, networks, worker count, and rate limiter
     - Returns list of IPs that responded to ICMP echo requests
     - For each responsive IP, calls `stateMgr.AddDevice(ip)` to add to state
     - If device is new (`isNew == true`), launches background goroutine to perform immediate SNMP scan
     - SNMP results are written to StateManager and InfluxDB via `writer.WriteDeviceInfo()`
   - **Concurrency:** SNMP scans run in background goroutines with panic recovery to avoid blocking the discovery loop
   - **Memory Check:** Calls `checkMemoryUsage()` before each scan to warn if memory exceeds configured limit

2. **Daily SNMP Scan Ticker** (`dailySNMPChan`)
   - **Schedule:** Configurable via `cfg.SNMPDailySchedule` in HH:MM format (e.g., "02:00")
   - **Purpose:** Performs full SNMP scan of all known devices at a scheduled time each day
   - **Operation:**
     - Retrieves all device IPs from StateManager via `stateMgr.GetAllIPs()`
     - Calls `discovery.RunSNMPScan()` with all IPs and SNMP configuration
     - Updates StateManager with hostname and sysDescr via `stateMgr.UpdateDeviceSNMP()`
     - Writes device info to InfluxDB via `writer.WriteDeviceInfo()`
     - Logs success and failure counts for visibility
   - **Implementation:** Uses `createDailySNMPChannel()` function that creates a goroutine calculating time until next scheduled run (24-hour wraparound handling)
   - **Optional:** Disabled if `cfg.SNMPDailySchedule` is empty string (creates dummy channel that never fires)

3. **Pinger Reconciliation Ticker** (`reconciliationTicker`)
   - **Interval:** Fixed 5 seconds
   - **Purpose:** Ensures every device in StateManager has an active continuous pinger goroutine
   - **Operation:**
     - Acquires `pingersMu` lock for thread-safe access to `activePingers` map and `stoppingPingers` map
     - Retrieves current IPs from StateManager and builds map for fast lookup
     - **Start New Pingers:** For each IP in StateManager:
       - Checks if IP is not in `activePingers` AND not in `stoppingPingers`
       - Respects `cfg.MaxConcurrentPingers` limit (logs warning if reached)
       - Creates child context from main context with `context.WithCancel()`
       - Stores cancel function in `activePingers[ip]`
       - Increments `pingerWg` before starting goroutine
       - Launches wrapper goroutine that calls `monitoring.StartPinger()` and notifies `pingerExitChan` on completion
     - **Stop Removed Pingers:** For each IP in `activePingers`:
       - If IP is not in current StateManager IPs, device was removed (pruned)
       - Moves IP to `stoppingPingers[ip] = true` before calling cancel function
       - Removes IP from `activePingers` map
       - Calls `cancelFunc()` to signal pinger to stop (asynchronous, doesn't wait for exit)
     - Releases `pingersMu` lock
   - **Race Prevention:** The `stoppingPingers` map prevents race condition where a device is pruned and quickly re-discovered before old pinger fully exits. A separate goroutine listens on `pingerExitChan` and removes IPs from `stoppingPingers` when pingers confirm exit.
   - **Concurrency Safety:** All map access protected by `pingersMu` mutex

4. **State Pruning Ticker** (`pruningTicker`)
   - **Interval:** Fixed 1 hour
   - **Purpose:** Removes devices that haven't been seen (successful ping) in the last 24 hours
   - **Operation:**
     - Calls `stateMgr.PruneStale(24 * time.Hour)`
     - Returns list of pruned devices
     - Logs each pruned device at debug level with IP and hostname
     - Logs summary at info level if any devices were pruned
   - **Integration:** Reconciliation ticker automatically detects removed devices and stops their pingers in next cycle (within 5 seconds)

5. **Health Report Ticker** (`healthReportTicker`)
   - **Interval:** Configurable via `cfg.HealthReportInterval` (default: 10s)
   - **Purpose:** Writes application health and observability metrics to InfluxDB health bucket
   - **Operation:**
     - Calls `healthServer.GetHealthMetrics()` to collect current metrics
     - Loads `totalPingsSent` atomic counter value
     - Calls `writer.WriteHealthMetrics()` with device count, active pingers, goroutines, memory (heap), RSS memory, suspended devices, InfluxDB status, batch success/failure counts, and total pings sent
   - **Metrics Written:** Device count, active pinger count (from `currentInFlightPings.Load()`), total goroutines, heap memory MB, RSS memory MB, suspended device count, InfluxDB connectivity status, successful batch count, failed batch count, total pings sent

### Concurrency Model

The application uses a comprehensive concurrency model to ensure thread-safety and graceful shutdown across all components:

- **Context-Based Cancellation:** 
  - Main context created with `context.WithCancel(context.Background())`
  - All child operations (discovery, SNMP scans, pingers) receive contexts derived from main context
  - Signal handler (SIGINT, SIGTERM) calls `stop()` function which cancels main context
  - Context cancellation propagates to all active goroutines, triggering coordinated shutdown
  
- **WaitGroup Tracking (`pingerWg`):**
  - Tracks all pinger goroutines for graceful shutdown
  - `pingerWg.Add(1)` called before starting each pinger wrapper goroutine
  - `defer pingerWg.Done()` in `monitoring.StartPinger()` ensures count decremented when pinger exits
  - Shutdown sequence calls `pingerWg.Wait()` to block until all pingers confirm exit
  
- **Mutex Protection (`pingersMu`):**
  - `sync.Mutex` protects concurrent access to:
    - `activePingers` map (IP string → context.CancelFunc)
    - `stoppingPingers` map (IP string → bool)
  - Locked during reconciliation loop when starting/stopping pingers
  - Locked when removing IPs from `stoppingPingers` via exit notification handler
  
- **Atomic Counters:**
  - `currentInFlightPings` (`atomic.Int64`): Tracks active pinger count for health metrics and accurate observability
  - `totalPingsSent` (`atomic.Uint64`): Tracks cumulative pings sent across all devices for metrics
  - Lock-free atomic operations for high-frequency updates without contention
  
- **Panic Recovery:**
  - All long-running goroutines wrapped with `defer func() { recover() }` pattern
  - Includes: discovery workers, SNMP scan workers, pingers, shutdown handler, daily SNMP scheduler, pinger exit notification handler
  - Panic recovery logs error with context (IP, operation type) and continues operation
  - Prevents single goroutine panic from crashing entire service
  
- **Non-Blocking Operations:**
  - SNMP scans for newly discovered devices run in background goroutines to avoid blocking discovery loop
  - Pinger exit notifications use buffered channel (`pingerExitChan`, capacity 100) to prevent blocking pinger shutdown
  - Rate limiter uses `golang.org/x/time/rate` package for non-blocking ping rate control

### Initialization Sequence

The application follows this strict initialization sequence in `main()`:

1. Parse `-config` CLI flag (default: "config.yml")
2. Initialize zerolog structured logging via `logger.Setup(false)` (debug mode disabled)
3. Load configuration from YAML file via `config.LoadConfig(*configPath)`
4. Validate configuration with `config.ValidateConfig(cfg)` (security and sanity checks, logs warnings)
5. Create StateManager with LRU eviction: `state.NewManager(cfg.MaxDevices)`
6. Create InfluxDB writer with batching: `influx.NewWriter()` with URL, token, org, bucket, health bucket, batch size, flush interval
7. Perform InfluxDB health check via `writer.HealthCheck()` (fail-fast with `log.Fatal()` on error)
8. Initialize global ping rate limiter: `rate.NewLimiter(rate.Limit(cfg.PingRateLimit), cfg.PingBurstLimit)`
9. Initialize atomic counters: `currentInFlightPings` and `totalPingsSent`
10. Initialize concurrency primitives:
    - `activePingers` map (IP → cancel function)
    - `stoppingPingers` map (IP → bool)
    - `pingersMu` mutex
    - `pingerWg` WaitGroup
    - `pingerExitChan` buffered channel (capacity 100)
11. Start health check HTTP server with callbacks for dynamic pinger count and total pings sent
12. Setup signal handling: `signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)`
13. Create main context with cancel function: `mainCtx, stop := context.WithCancel(context.Background())`
14. Create all five tickers (ICMP discovery, daily SNMP, reconciliation, pruning, health report)
15. Run initial ICMP discovery scan before entering main event loop
16. Start shutdown handler goroutine (listens for signals)
17. Start pinger exit notification handler goroutine (removes IPs from `stoppingPingers`)
18. Enter main event loop (select statement with all ticker cases)

### Graceful Shutdown Sequence

When shutdown signal (SIGINT or SIGTERM) is received, the following sequence executes:

1. Signal received on `sigChan` in shutdown handler goroutine
2. Shutdown handler calls `stop()` function, canceling main context (`mainCtx`)
3. Main event loop receives `<-mainCtx.Done()` in select case, enters shutdown block
4. Stop all five tickers explicitly via `.Stop()` calls:
   - `icmpDiscoveryTicker.Stop()`
   - `reconciliationTicker.Stop()`
   - `pruningTicker.Stop()`
   - (Daily SNMP ticker is already wrapped in goroutine)
   - (Health report ticker stopped implicitly)
5. Acquire `pingersMu` lock for exclusive access
6. Iterate `activePingers` map and call all cancel functions:
   - `for ip, cancel := range activePingers { cancel() }`
   - Each `cancel()` triggers context cancellation in corresponding pinger
7. Release `pingersMu` lock
8. Call `pingerWg.Wait()` to block until all pinger goroutines exit:
   - Each pinger checks `ctx.Done()` and exits gracefully
   - Each pinger calls `defer pingerWg.Done()` on exit
9. Call `writer.Close()` to flush remaining batched points:
   - Cancels batch flusher context
   - Drains points from batch channel
   - Flushes remaining points to both WriteAPIs (primary and health buckets)
   - Closes InfluxDB client
10. Log "Shutdown complete" and return from `main()`

---

## Technology Stack

**Language:** Go 1.25.1
- Module: `github.com/kljama/netscan` (as specified in `go.mod`)
- Go version requirement: `go 1.25.1` (minimum version)

**Primary Dependencies (from `go.mod`):**

- **`gopkg.in/yaml.v3 v3.0.1`** - YAML configuration file parsing
  - Used in `config.LoadConfig()` to parse `config.yml`
  - Supports struct tags for mapping YAML fields to Go structs
  
- **`github.com/gosnmp/gosnmp v1.42.1`** - SNMPv2c protocol implementation
  - Used in `discovery.RunSNMPScan()` for querying device metadata
  - Supports Get, GetNext, and Walk operations
  - Queries sysName (hostname) and sysDescr (system description) OIDs
  
- **`github.com/prometheus-community/pro-bing v0.7.0`** - ICMP ping implementation with raw socket support
  - Used in `discovery.RunICMPSweep()` for device discovery
  - Used in `monitoring.StartPinger()` for continuous uptime monitoring
  - Requires CAP_NET_RAW capability or root privileges for raw ICMP sockets
  
- **`github.com/influxdata/influxdb-client-go/v2 v2.14.0`** - InfluxDB v2 client with WriteAPI
  - Used in `influx.NewWriter()` for batched time-series writes
  - Supports dual-bucket writes (primary metrics + health metrics)
  - Provides non-blocking WriteAPI with background flushing
  
- **`github.com/rs/zerolog v1.34.0`** - Zero-allocation structured logging (JSON and console)
  - Configured in `logger.Setup()` with service name, timestamp, and caller info
  - Supports debug/info/warn/error/fatal levels
  - Console output when `ENVIRONMENT=development` environment variable set
  - Debug level enabled via `debugMode` parameter or `DEBUG=true` environment variable
  - Adds caller information (file:line) to all log entries for debugging
  
- **`golang.org/x/time v0.14.0`** - Rate limiting utilities
  - Used to create `rate.NewLimiter()` for global ping rate limiting
  - Controls sustained ICMP ping rate across all devices
  - Prevents network flooding and resource exhaustion

**Standard Library Usage:**

- **`sync`** - Concurrency primitives
  - `sync.Mutex` / `sync.RWMutex` - Protects shared maps (activePingers, stoppingPingers, StateManager devices)
  - `sync.WaitGroup` - Tracks pinger goroutine lifecycle
  - `sync/atomic` - Lock-free atomic counters for metrics
  
- **`context`** - Cancellation propagation and timeout control
  - Main context with `context.WithCancel()` for graceful shutdown
  - Child contexts for pingers, discovery, and SNMP scans
  - Timeout contexts for individual operations
  
- **`time`** - Time-based operations
  - `time.NewTicker()` - Five independent ticker loops
  - `time.Duration` - Interval configuration
  - `time.Parse()` - Daily SNMP schedule parsing (HH:MM format)
  
- **`flag`** - CLI argument parsing
  - Single `-config` flag for configuration file path (default: "config.yml")
  
- **`net`** - IP address validation and parsing
  - Used in device validation and network operations
  - IP format validation and address type checking
  
- **`os/signal`** - Signal handling for graceful shutdown
  - `signal.Notify()` - Listens for SIGINT and SIGTERM
  - Triggers context cancellation on signal receipt

---

## Deployment Model

### Docker Deployment (Primary - Recommended)

**Multi-Stage Dockerfile:**
- **Builder Stage:** Uses `golang:1.25-alpine` as the build environment
  - Installs build dependencies: `git`, `ca-certificates`
  - Compiles binary with `CGO_ENABLED=0 GOOS=linux GOARCH=amd64`
  - Binary stripping via `-ldflags="-w -s"` to minimize size and remove debug symbols
  
- **Runtime Stage:** Uses `alpine:latest` for minimal production image
  - Creates non-root user `netscan` with dedicated group
  - Copies only the compiled binary from builder stage
  - Includes config template (`config.yml.example`)

**Runtime Stage Packages:**
- `ca-certificates` - TLS/SSL certificate validation for HTTPS connections
- `libcap` - Linux capability management utilities (provides `setcap`)
- `wget` - HTTP client for health check endpoint testing

**Capabilities:**
- **Dockerfile:** Sets `cap_net_raw+ep` capability on binary via `setcap cap_net_raw+ep /app/netscan`
  - `cap_net_raw` - Allows raw ICMP socket creation for ping operations
  - `+ep` flags - Effective and Permitted capability sets
- **docker-compose.yml:** Adds `NET_RAW` capability to container via `cap_add: - NET_RAW`
  - Grants container permission to create raw sockets at runtime
  - Required for ICMP echo request/reply functionality

**Security Note:**
- **Runtime User:** Container runs as `root` (non-root user commented out in Dockerfile)
- **Reason:** Linux kernel security model in containerized environments requires root privileges for raw ICMP socket access, even with `CAP_NET_RAW` capability set
- **Limitation:** Non-root users cannot create raw ICMP sockets in Docker containers despite capability grants
- **Comment in Dockerfile:** Lines 48-51 explain this security constraint

**Docker Compose Stack:**
- **Services:**
  - `netscan` - Network monitoring application (builds from Dockerfile)
  - `influxdb` - InfluxDB v2.7 time-series database for metrics storage
- **Service Dependencies:** `netscan` depends on `influxdb` with `condition: service_healthy`

**Network Mode:**
- **Configuration:** `network_mode: host` on netscan service
- **Purpose:** Provides direct access to host network stack for ICMP and SNMP operations
- **Impact:** Container shares host's network namespace, enabling network device discovery on local subnets

**Configuration:**
- **Config Mount:** `./config.yml:/app/config.yml:ro` (read-only)
- **Location:** Config file must exist in same directory as `docker-compose.yml`
- **Preparation:** Copy from template with `cp config.yml.example config.yml`
- **Environment Variables:** Loaded from `.env` file for credential management:
  - `INFLUXDB_TOKEN` (default: `netscan-token`)
  - `INFLUXDB_ORG` (default: `test-org`)
  - `SNMP_COMMUNITY` (default: `public`)
  - `DEBUG` (default: `false`)
  - `ENVIRONMENT` (default: `development`)

**Health Checks:**
- **Dockerfile HEALTHCHECK:**
  - Command: `wget --no-verbose --tries=1 --spider http://localhost:8080/health/live || exit 1`
  - Interval: 30 seconds
  - Timeout: 3 seconds
  - Start period: 40 seconds (grace period for startup)
  - Retries: 3 consecutive failures before marking unhealthy
  
- **docker-compose.yml healthcheck:**
  - Command: `["CMD", "wget", "--spider", "-q", "http://localhost:8080/health/live"]`
  - Same timing parameters as Dockerfile
  - Tests HTTP endpoint at `/health/live` on port 8080

**Log Rotation:**
- Driver: `json-file`
- Max size: `10m` per log file
- Max files: `3` retained files (~30MB total storage)

### Native systemd Deployment (Alternative - Maximum Security)

**Security Model:**
- **Dedicated System User:**
  - Creates `netscan` system user via `useradd -r -s /bin/false netscan`
  - `-r` flag: Creates system account (UID < 1000)
  - `-s /bin/false`: Disables shell login for security
  
- **Capability-Based Security:**
  - Command: `setcap cap_net_raw+ep /opt/netscan/netscan`
  - Grants only `CAP_NET_RAW` capability to binary (principle of least privilege)
  - Eliminates need for full root privileges
  - Capability persists across executions
  
- **Environment File Security:**
  - Location: `/opt/netscan/.env`
  - Permissions: `600` (owner read/write only)
  - Owner: `netscan:netscan` system user
  - Contains sensitive credentials (InfluxDB token, SNMP community string)
  - Automatically loaded by systemd service via `EnvironmentFile` directive

**Installation Location:**
- Base directory: `/opt/netscan/`
- Binary: `/opt/netscan/netscan`
- Configuration: `/opt/netscan/config.yml`
- Environment: `/opt/netscan/.env`
- Systemd service: `/etc/systemd/system/netscan.service`

**Systemd Service Hardening:**
The `deploy.sh` script generates a systemd service file with the following security features:

- **`NoNewPrivileges=yes`** - Prevents privilege escalation via setuid/setgid binaries
- **`PrivateTmp=yes`** - Provides isolated `/tmp` directory (not shared with host)
- **`ProtectSystem=strict`** - Makes entire filesystem read-only except specific writable paths
- **`AmbientCapabilities=CAP_NET_RAW`** - Grants only raw socket capability to process
- **`User=$SERVICE_USER` / `Group=$SERVICE_USER`** - Runs as dedicated non-root `netscan` user
- **`Restart=always`** - Automatic restart on failure for high availability
- **`EnvironmentFile=/opt/netscan/.env`** - Securely loads credentials from protected file

**Service Management:**
- Enable: `systemctl enable netscan`
- Start: `systemctl start netscan`
- Status: `systemctl status netscan`
- Logs: `journalctl -u netscan -f`

---

## Core Components (Features)

### Configuration System (`internal/config/config.go`)

**YAML Configuration Loading:**
- Configuration loaded from `config.yml` file via `LoadConfig(path string)` function
- Uses `gopkg.in/yaml.v3` decoder for parsing YAML to Go structs
- **Environment Variable Expansion:** Applies `os.ExpandEnv()` to sensitive fields after YAML parsing:
  - `influxdb.url`, `influxdb.token`, `influxdb.org`, `influxdb.bucket`, `influxdb.health_bucket`
  - `snmp.community`
- Supports `${VAR}` and `$VAR` syntax for environment variable substitution
- Duration parsing from string format (e.g., "5m", "1h") to `time.Duration` type

**Validation:**
- `ValidateConfig(cfg *Config)` performs security and sanity checks
- Returns `(warning string, error)` tuple - warnings for security concerns, errors for validation failures
- **Security Validations:**
  - CIDR network range validation (rejects loopback, multicast, link-local, overly broad ranges)
  - SNMP community string validation (character restrictions, weak password detection)
  - InfluxDB URL validation (http/https scheme enforcement, URL format checks)
  - IP address validation for network ranges
- **Sanity Checks:**
  - Worker count limits (ICMP: 1-2000, SNMP: 1-1000)
  - Interval minimums (discovery: 1min, ping: 1s)
  - Resource protection limits (max devices: 1-100000, memory: 64-16384 MB)
  - Time format validation for daily SNMP schedule (HH:MM format)

**Configuration Parameters with Defaults:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| **Main Config** | | | |
| `discovery_interval` | `time.Duration` | `4h` | Legacy discovery interval (backward compatibility) |
| `icmp_discovery_interval` | `time.Duration` | (required) | Interval for ICMP network discovery sweeps |
| `icmp_workers` | `int` | `64` | Number of concurrent ICMP discovery workers |
| `snmp_workers` | `int` | `32` | Number of concurrent SNMP scan workers |
| `networks` | `[]string` | (required) | List of CIDR network ranges to scan |
| `ping_interval` | `time.Duration` | (required) | Interval between continuous pings per device |
| `ping_timeout` | `time.Duration` | `3s` | Timeout for individual ping operations |
| `ping_rate_limit` | `float64` | `64.0` | Sustained ping rate in pings per second (token bucket rate) |
| `ping_burst_limit` | `int` | `256` | Maximum burst ping capacity (token bucket size) |
| `ping_max_consecutive_fails` | `int` | `10` | Circuit breaker: consecutive failures before suspension |
| `ping_backoff_duration` | `time.Duration` | `5m` | Circuit breaker: suspension duration after threshold |
| `snmp_daily_schedule` | `string` | (optional) | Daily SNMP scan time in HH:MM format (e.g., "02:00") |
| `health_check_port` | `int` | `8080` | HTTP port for health check endpoint |
| `health_report_interval` | `time.Duration` | `10s` | Interval for writing health metrics to InfluxDB |
| `max_concurrent_pingers` | `int` | `20000` | Maximum number of concurrent pinger goroutines |
| `max_devices` | `int` | `20000` | Maximum devices managed by StateManager (LRU eviction) |
| `min_scan_interval` | `time.Duration` | `1m` | Minimum time between discovery scans |
| `memory_limit_mb` | `int` | `16384` | Memory limit in MB (warning threshold) |
| **SNMP Config** | | | |
| `snmp.community` | `string` | (required) | SNMPv2c community string |
| `snmp.port` | `int` | (required) | SNMP port (typically 161) |
| `snmp.timeout` | `time.Duration` | `5s` | SNMP request timeout |
| `snmp.retries` | `int` | (required) | Number of SNMP retry attempts |
| **InfluxDB Config** | | | |
| `influxdb.url` | `string` | (required) | InfluxDB server URL (http:// or https://) |
| `influxdb.token` | `string` | (required) | InfluxDB authentication token |
| `influxdb.org` | `string` | (required) | InfluxDB organization name |
| `influxdb.bucket` | `string` | (required) | Primary bucket for ping/device metrics |
| `influxdb.health_bucket` | `string` | `"health"` | Bucket for application health metrics |
| `influxdb.batch_size` | `int` | `5000` | Number of points to batch before writing |
| `influxdb.flush_interval` | `time.Duration` | `5s` | Maximum time to hold points before flushing |

### State Management (`internal/state/manager.go`)

**Thread-Safe Device Registry:**
- `Manager` struct provides centralized device state storage
- **Concurrency Control:** `sync.RWMutex` (`mu` field) protects all map operations
  - Read operations use `RLock()`/`RUnlock()` for concurrent read access
  - Write operations use `Lock()`/`Unlock()` for exclusive write access
- **Device Storage:** `devices map[string]*Device` - maps IP addresses to device pointers
- **Capacity Management:** `maxDevices int` - enforces device count limit with LRU eviction

**Device Struct:**
- `IP string` - IPv4 address of the device (map key)
- `Hostname string` - Device hostname from SNMP or IP address as fallback
- `SysDescr string` - SNMP sysDescr MIB-II value (system description)
- `LastSeen time.Time` - Timestamp of last successful ping or discovery
- `ConsecutiveFails int` - Circuit breaker: counter for consecutive ping failures
- `SuspendedUntil time.Time` - Circuit breaker: timestamp when suspension expires

**Public Methods:**

- **`NewManager(maxDevices int) *Manager`**
  - Constructor: Creates new state manager with device capacity limit
  - Default: 10000 devices if maxDevices <= 0
  
- **`Add(device Device)`**
  - Adds or updates complete device struct
  - Idempotent: updates existing device if IP already exists
  - LRU eviction: removes oldest device (by LastSeen) when maxDevices reached
  
- **`AddDevice(ip string) bool`**
  - Adds device by IP only, minimal initialization
  - Returns `true` if new device, `false` if already exists
  - Sets Hostname to IP, LastSeen to current time
  - LRU eviction: same as Add() method
  
- **`Get(ip string) (*Device, bool)`**
  - Retrieves device by IP address
  - Returns `(device, true)` if found, `(nil, false)` if not found
  
- **`GetAll() []Device`**
  - Returns copy of all devices as slice (value copies, not pointers)
  - Safe for iteration without holding lock
  
- **`GetAllIPs() []string`**
  - Returns slice of all device IP addresses
  - Used by reconciliation loop and daily SNMP scan
  
- **`UpdateLastSeen(ip string)`**
  - Updates LastSeen timestamp to current time
  - Called on successful ping to mark device as alive
  
- **`UpdateDeviceSNMP(ip, hostname, sysDescr string)`**
  - Enriches device with SNMP metadata
  - Updates Hostname, SysDescr, and LastSeen fields
  
- **`PruneStale(olderThan time.Duration) []Device`**
  - Removes devices not seen within duration (e.g., 24 hours)
  - Returns slice of removed devices for logging
  - Alias: `Prune()` - same functionality
  
- **`Count() int`**
  - Returns current number of managed devices

- **`ReportPingSuccess(ip string)`**
  - Circuit breaker: resets failure counter and clears suspension
  - Sets ConsecutiveFails to 0, SuspendedUntil to zero time
  
- **`ReportPingFail(ip string, maxFails int, backoff time.Duration) bool`**
  - Circuit breaker: increments failure counter
  - Returns `true` if device was suspended (threshold reached)
  - On suspension: resets counter, sets SuspendedUntil to now + backoff
  
- **`IsSuspended(ip string) bool`**
  - Checks if device is currently suspended by circuit breaker
  - Returns `true` if SuspendedUntil is set and in the future
  
- **`GetSuspendedCount() int`**
  - Returns count of currently suspended devices
  - Used for health metrics reporting

**LRU Eviction:**
- Triggered in `Add()` and `AddDevice()` when `len(devices) >= maxDevices`
- **Eviction Algorithm:**
  1. Iterate all devices to find oldest LastSeen timestamp
  2. Delete device with smallest (oldest) LastSeen time
  3. Add new device to freed slot
- **Guarantees:** Device count never exceeds maxDevices limit
- **Trade-off:** O(n) eviction time for simplicity (no heap/priority queue)

### InfluxDB Writer (`internal/influx/writer.go`)

**High-Performance Batch System:**
- **Architecture:** Channel-based lock-free design with background flusher goroutine
- **Components:**
  - `batchChan chan *write.Point` - Buffered channel (capacity: 2x batch size)
  - `backgroundFlusher()` - Goroutine that accumulates and flushes points
  - `flushTicker *time.Ticker` - Triggers time-based flushes at `flushInterval`
- **Batching Logic:**
  - Accumulates points in local slice until batch size reached OR flush interval elapsed
  - Size-based flush: immediately when batch reaches `batchSize` points
  - Time-based flush: every `flushInterval` even if batch incomplete
  - Non-blocking writes: drops points if channel full (logs warning)

**Dual-Bucket Architecture:**
- **Primary WriteAPI** (`writeAPI`): Writes ping results and device info to main bucket
- **Health WriteAPI** (`healthWriteAPI`): Writes application health metrics to separate health bucket
- **Rationale:** Separates operational metrics from application monitoring data
- **Error Monitoring:** Each WriteAPI has dedicated error channel monitored by `monitorWriteErrors()` goroutine

**Constructor:**
- **`NewWriter(url, token, org, bucket, healthBucket string, batchSize int, flushInterval time.Duration) *Writer`**
  - Creates InfluxDB client with dual WriteAPI instances
  - Initializes buffered batch channel (capacity: `batchSize * 2`)
  - Starts background flusher goroutine immediately
  - Obtains error channels once during initialization (stored for reuse)
  - Returns Writer with context-based cancellation support

**HealthCheck():**
- Verifies InfluxDB connectivity using client health API
- 5-second timeout via context
- Returns error if health status is not "pass"
- Called during application startup (fail-fast if InfluxDB unavailable)

**Batching Architecture Details:**

- **`batchChan chan *write.Point`**
  - Buffered channel for lock-free point submission
  - Capacity: 2x batch size to prevent blocking during normal operation
  - Writers use non-blocking send with default case (drops on full)

- **`batchSize int`**
  - Number of points to accumulate before flushing
  - Default: 5000 points (configurable via InfluxDB config)
  - Triggers immediate flush when reached

- **`flushInterval time.Duration`**
  - Maximum time to hold points before flushing
  - Default: 5 seconds (configurable via InfluxDB config)
  - Ensures timely data delivery even with low write rates

- **Background Flusher:**
  - Single goroutine with panic recovery
  - Select loop handles: context cancellation, flush timer, new points
  - Accumulates points in local slice (no mutex needed)
  - Flushes to InfluxDB via `flushWithRetry()` with exponential backoff

**Graceful Shutdown:**
- **`Close()` Method:**
  1. Cancels context - signals background flusher to stop
  2. Stops flush ticker
  3. Waits 100ms for background flusher to finish
  4. Background flusher calls `drainAndFlush()` - empties channel and flushes remaining points
  5. Flushes both WriteAPI buffers (primary and health)
  6. Closes InfluxDB client connection
- **Guarantees:** No data loss on graceful shutdown - all queued points flushed

**Write Methods:**

- **`WritePingResult(ip string, rtt time.Duration, successful bool) error`**
  - **Measurement:** `"ping"`
  - **Tags:** `ip` (device IP address)
  - **Fields:**
    - `rtt_ms` (float64): Round-trip time in milliseconds
    - `success` (bool): Ping success/failure status
  - **Validation:** IP address format, RTT range (0 to 1 minute)
  - **Batching:** Adds to batch channel via `addToBatch()`

- **`WriteDeviceInfo(ip, hostname, sysDescr string) error`**
  - **Measurement:** `"device_info"`
  - **Tags:** `ip` (device IP address)
  - **Fields:**
    - `hostname` (string): Device hostname from SNMP
    - `snmp_description` (string): SNMP sysDescr value
  - **Validation:** IP address format
  - **Sanitization:** Applies `sanitizeInfluxString()` to hostname and sysDescr
  - **Batching:** Adds to batch channel via `addToBatch()`

- **`WriteHealthMetrics(deviceCount, pingerCount, goroutines, memMB, rssMB, suspendedCount int, influxOK bool, influxSuccess, influxFailed, pingsSentTotal uint64)`**
  - **Measurement:** `"health_metrics"`
  - **Tags:** None (application-level metrics)
  - **Fields:**
    - `device_count` (int): Total devices in StateManager
    - `active_pingers` (int): Currently running pinger goroutines
    - `suspended_devices` (int): Devices suspended by circuit breaker
    - `goroutines` (int): Total Go goroutines
    - `memory_mb` (int): Heap memory usage in MB
    - `rss_mb` (int): OS-level resident set size in MB
    - `influxdb_ok` (bool): InfluxDB connectivity status
    - `influxdb_successful_batches` (uint64): Cumulative successful batch writes
    - `influxdb_failed_batches` (uint64): Cumulative failed batch writes
    - `pings_sent_total` (uint64): Total pings sent since startup
  - **Write Path:** Bypasses batch channel, writes directly to `healthWriteAPI`
  - **Rationale:** Health metrics written on fixed interval, no need for batching

**Data Sanitization:**
- **`sanitizeInfluxString(s, fieldName string) string`**
  - **Purpose:** Prevents database corruption and injection attacks
  - **Operations:**
    - Length limiting: truncates to 500 characters, appends "..."
    - Control character removal: strips characters < 32 (except tab and newline)
    - Whitespace trimming: removes leading/trailing spaces
  - **Applied to:** hostname and sysDescr fields in WriteDeviceInfo()
  - **Logging:** Could log when string significantly modified (currently unused to avoid noise)

**Metrics Tracking:**
- `successfulBatches atomic.Uint64` - Counter for successful batch writes
- `failedBatches atomic.Uint64` - Counter for failed batch writes
- Atomic operations ensure thread-safe updates from background flusher
- Exposed via `GetSuccessfulBatches()` and `GetFailedBatches()` for health reporting

**Error Handling:**
- `monitorWriteErrors()` goroutine continuously monitors error channels
- Logs errors with bucket context (primary or health)
- Retry logic in `flushWithRetry()`:
  - Up to 3 retry attempts with exponential backoff (1s, 2s, 4s)
  - Increments failed batch counter on final failure
  - Increments successful batch counter on success

### ICMP Discovery (`internal/discovery/scanner.go`)

**Function Signature:**
```go
func RunICMPSweep(ctx context.Context, networks []string, workers int, limiter *rate.Limiter) []string
```

**Worker Pool Pattern:**
- Creates `jobs` channel (buffered: 256) for IP addresses to ping
- Creates `results` channel (buffered: 256) for responsive IPs
- Launches `workers` goroutines (default: 64) that consume from `jobs` channel
- Each worker:
  - Acquires token from rate limiter via `limiter.Wait(ctx)` before pinging
  - Creates pinger with `probing.NewPinger(ip)`
  - Sends single ICMP echo request (1 second timeout)
  - Sends responsive IP to `results` channel if `stats.PacketsRecv > 0`
- Producer goroutine streams IPs from all networks to `jobs` channel, then closes it
- Wait goroutine waits for all workers via `WaitGroup`, then closes `results` channel
- Main function collects all responsive IPs from `results` channel and returns slice

**Implementation Details:**
- **`streamIPsFromCIDR(network string, ipChan chan<- string)`**:
  - Streams IP addresses from CIDR notation directly to channel
  - Avoids allocating memory for all IPs at once (memory-efficient for large networks)
  - Parses CIDR with `net.ParseCIDR()`
  - Iterates through subnet using `incIP()` helper function
  - Logs warning for networks larger than /16 (65K hosts)
  - Safety limit: max 65,536 IPs per network
- **`SetPrivileged(true)`**:
  - Configures pinger to use raw ICMP sockets
  - Requires CAP_NET_RAW capability or root privileges
  - Necessary for sending/receiving ICMP echo request/reply packets

### SNMP Scanning (`internal/discovery/scanner.go`)

**Function Signature:**
```go
func RunSNMPScan(ips []string, snmpConfig *config.SNMPConfig, workers int) []state.Device
```

**Worker Pool Pattern:**
- Creates `jobs` channel (buffered: 256) for IP addresses to scan
- Creates `results` channel (buffered: 256) for discovered devices
- Launches `workers` goroutines (default: 32) that consume from `jobs` channel
- Each worker:
  - Configures `gosnmp.GoSNMP` with target IP, port, community, version, timeout, retries
  - Connects via `params.Connect()`
  - Queries standard MIB-II OIDs using `snmpGetWithFallback()`
  - Validates and sanitizes SNMP responses via `validateSNMPString()`
  - Sends `state.Device` with IP, Hostname, SysDescr, LastSeen to `results` channel
- Producer goroutine enqueues all IPs to `jobs` channel, then closes it
- Wait goroutine waits for all workers via `WaitGroup`, then closes `results` channel
- Main function collects all discovered devices from `results` channel and returns slice

**SNMP Robustness Features:**

- **`snmpGetWithFallback(params *gosnmp.GoSNMP, oids []string) (*gosnmp.SnmpPacket, error)`**:
  - **Primary Strategy:** Attempts `params.Get(oids)` first (most efficient for .0 instances)
  - **Fallback Strategy:** If Get returns `NoSuchInstance`/`NoSuchObject`, tries `params.GetNext()` for each OID
  - **Rationale:** Some devices don't support .0 instance OIDs, GetNext retrieves next OID in tree
  - **Validation:** Verifies returned OID has the requested base OID as prefix
  - **Error Handling:** Returns error if no valid SNMP data retrieved from either method

- **`validateSNMPString(value interface{}, oidName string) (string, error)`**:
  - **Type Handling:** Accepts both `string` and `[]byte` types (SNMP OctetString values)
  - **Conversion:** Converts `[]byte` to string via `string(v)`
  - **Security Checks:**
    - Rejects strings containing null bytes (`\x00`)
    - Limits length to 1024 characters (truncates to prevent memory exhaustion)
  - **Sanitization:**
    - Replaces newlines/tabs (`\n`, `\r`, `\t`) with spaces
    - Removes non-printable ASCII characters (< 32 or > 126)
    - Trims whitespace
  - **Validation:** Returns error if string is empty after sanitization

**Queried OIDs:**
- **`1.3.6.1.2.1.1.5.0`** - `sysName` (device hostname)
- **`1.3.6.1.2.1.1.1.0`** - `sysDescr` (system description from MIB-II)

### Continuous Monitoring (`internal/monitoring/pinger.go`)

**Function Signature:**
```go
func StartPinger(ctx context.Context, wg *sync.WaitGroup, device state.Device, interval time.Duration, timeout time.Duration, writer PingWriter, stateMgr StateManager, limiter *rate.Limiter, inFlightCounter *atomic.Int64, totalPingsSent *atomic.Uint64, maxConsecutiveFails int, backoffDuration time.Duration)
```

**Lifecycle:**
- Runs as dedicated goroutine per device (one pinger per monitored device)
- Uses `time.NewTimer()` for scheduling pings at configured intervals
- Defers `wg.Done()` to signal completion when goroutine exits
- Includes panic recovery with `defer func() { recover() }` pattern
- Continues until context is cancelled via `<-ctx.Done()`

**Operation:**

1. **Circuit Breaker Check:**
   - Calls `stateMgr.IsSuspended(device.IP)` before acquiring rate limiter token
   - Skips ping if device is suspended (circuit breaker tripped)
   - Logs debug message and resets timer for next interval

2. **Rate Limiting:**
   - Acquires token from global rate limiter via `limiter.Wait(ctx)`
   - Blocks until token available or context cancelled
   - Ensures compliance with global ping rate limit across all devices

3. **Ping Execution:**
   - Calls `performPingWithCircuitBreaker()` with device, timeout, writer, state manager, counters, circuit breaker params
   - Increments `inFlightCounter` (atomic) at start, decrements on completion
   - Increments `totalPingsSent` (atomic) for observability

4. **IP Validation** (`validateIPAddress(ipStr string) error`):
   - Checks IP is not empty
   - Parses IP with `net.ParseIP()`
   - **Security Checks:**
     - Rejects loopback addresses (`ip.IsLoopback()`)
     - Rejects multicast addresses (`ip.IsMulticast()`)
     - Rejects link-local addresses (`ip.IsLinkLocalUnicast()`)
     - Rejects unspecified addresses (`ip.IsUnspecified()`)
   - Returns error if any check fails

5. **Success Criteria:**
   - Determines success by checking `len(stats.Rtts) > 0 && stats.AvgRtt > 0`
   - More reliable than just `stats.PacketsRecv` as RTT measurements prove response received
   - `stats.Rtts` is slice of individual round-trip times
   - `stats.AvgRtt` is average RTT across all attempts

6. **State Updates on Success:**
   - Calls `stateMgr.ReportPingSuccess(ip)` to reset circuit breaker failure counter
   - Calls `stateMgr.UpdateLastSeen(ip)` to update LastSeen timestamp

7. **State Updates on Failure:**
   - Calls `stateMgr.ReportPingFail(ip, maxConsecutiveFails, backoffDuration)` to increment failure counter
   - Returns `true` if device was suspended (threshold reached)
   - Logs warning when circuit breaker trips

8. **Metrics Writing:**
   - Calls `writer.WritePingResult(ip, rtt, success)` with device IP, RTT, and success boolean
   - RTT is `stats.AvgRtt` for successful pings, `0` for failures
   - Logs error if write fails (non-fatal, continues monitoring)

**Interface Design:**

```go
// PingWriter interface for writing ping results to external storage
type PingWriter interface {
WritePingResult(ip string, rtt time.Duration, successful bool) error
WriteDeviceInfo(ip, hostname, sysDescr string) error
}

// StateManager interface for updating device last seen timestamp
type StateManager interface {
UpdateLastSeen(ip string)
ReportPingSuccess(ip string)
ReportPingFail(ip string, maxFails int, backoff time.Duration) bool
IsSuspended(ip string) bool
}
```

### Health Check Server (`cmd/netscan/health.go`)

**HTTP Server:**
- Server started in `Start()` method via `http.ListenAndServe()`
- Runs in background goroutine with panic recovery
- Binds to port specified by `health_check_port` config (default: 8080)
- Non-blocking: returns immediately after starting goroutine
- Logs startup with `log.Info().Str("address", addr).Msg("Health check endpoint started")`

**Three Endpoints:**

1. **`/health`** - Detailed health information
   - Returns comprehensive `HealthResponse` JSON with all metrics
   - Always returns HTTP 200 (provides status in JSON field)
   - Calls `GetHealthMetrics()` to gather current metrics

2. **`/health/ready`** - Readiness probe
   - Checks if service is ready to accept traffic
   - Tests InfluxDB connectivity via `writer.HealthCheck()`
   - Returns HTTP 200 with "READY" if InfluxDB accessible
   - Returns HTTP 503 with "NOT READY: InfluxDB unavailable" if InfluxDB down
   - Used by orchestrators to determine when to send traffic

3. **`/health/live`** - Liveness probe
   - Indicates if service process is alive
   - Always returns HTTP 200 with "ALIVE" if handler responds
   - Used by orchestrators to determine if container should be restarted
   - Simple check: if we can respond, we're alive

**Health Response Fields:**

| Field | Type | Description |
|-------|------|-------------|
| `status` | `string` | Overall status: "healthy" (InfluxDB OK) or "degraded" (InfluxDB down) |
| `version` | `string` | Version string (hardcoded "1.0.0", TODO: build-time variable) |
| `uptime` | `string` | Human-readable uptime since service start |
| `device_count` | `int` | Number of devices in StateManager |
| `suspended_devices` | `int` | Number of devices suspended by circuit breaker |
| `active_pingers` | `int` | Accurate count of active pinger goroutines (from `activePingers` map) |
| `influxdb_ok` | `bool` | InfluxDB connectivity status (true if healthy) |
| `influxdb_successful` | `uint64` | Cumulative successful InfluxDB batch writes |
| `influxdb_failed` | `uint64` | Cumulative failed InfluxDB batch writes |
| `pings_sent_total` | `uint64` | Total monitoring pings sent since startup |
| `goroutines` | `int` | Current Go goroutine count via `runtime.NumGoroutine()` |
| `memory_mb` | `uint64` | Go heap memory usage in MB (from `runtime.MemStats.Alloc`) |
| `rss_mb` | `uint64` | OS-level resident set size in MB (from `/proc/self/status`) |
| `timestamp` | `time.Time` | Current timestamp when metrics collected |

**Note on Memory Metrics:**

- **`memory_mb`** - Go heap allocation
  - Obtained from `runtime.MemStats.Alloc`
  - Represents memory allocated by Go runtime for heap objects
  - Only includes Go-managed memory (heap allocations)
  - Does not include stack memory, OS-level overhead, or memory-mapped files

- **`rss_mb`** - OS-level resident set size
  - Obtained via `getRSSMB()` function which reads `/proc/self/status`
  - Parses `VmRSS` field (in kB) and converts to MB
  - Represents total physical memory used by process (Linux-specific)
  - Includes: Go heap, stacks, memory-mapped files, shared libraries, OS overhead
  - More accurate reflection of actual memory consumption from OS perspective
  - Returns 0 on failure (non-Linux systems, permission issues, parse errors)
  - **Implementation:** Opens `/proc/self/status`, scans for line starting with "VmRSS:", parses value in kB, converts to MB
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
