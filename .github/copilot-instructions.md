Project Goal
netscan is a Go-based network monitoring service with a decoupled, multi-ticker architecture designed for containerized deployment via Docker Compose, with native deployment as an alternative option. The service follows this workflow:

Periodic ICMP Discovery: Scan configured network ranges to find new, active devices.

Continuous ICMP Monitoring: For every device found, initiate an immediate and continuous high-frequency (e.g., 1-second) ICMP ping to track real-time uptime.

Dual-Trigger SNMP Scanning:

Initial: Perform an SNMP query immediately after a device is first discovered.

Scheduled: Perform a full SNMP scan on all known-alive devices at a configurable daily time (e.g., 02:00 AM).

Resilient Data Persistence: Write all monitoring (ICMP) and discovery (SNMP) data to InfluxDB, with robust protections against network failures, data corruption, and database overload.

Deployment Architecture
Docker Deployment (Primary)
Recommended approach for ease of deployment and consistency

Multi-stage Dockerfile with minimal Alpine Linux runtime (~15MB)

Docker Compose orchestrates netscan + InfluxDB stack

Security Note: Container runs as root (required for ICMP raw socket access in Linux containers)

Host networking mode for direct network access

Environment variable support via docker-compose.yml or optional .env file

See README.md for Docker deployment instructions

Native Deployment (Alternative)
For maximum security with non-root service user

Uses dedicated netscan system user with no shell access

CAP_NET_RAW capability via setcap (no root required)

Systemd service with security restrictions

See README_NATIVE.md for native deployment instructions

🏛️ Guiding Principles for New Features
All new code and features must adhere to these principles:

Decoupled & Concurrent: New services (e.g., a new scanner type, a data export) MUST be implemented as decoupled, concurrent goroutines. They should be orchestrated by a dedicated Ticker in main.go and must not block other services.

Centralized State: The StateManager is the single source of truth for all device state. New features MUST interact with the StateManager via its thread-safe methods. Do not create separate device lists.

Resilience First: All new code interacting with external services (networks, databases, APIs) MUST implement:

Aggressive context.WithTimeout

Robust error handling (log the error and continue; never panic)

Client-side rate limiting where appropriate.

Configurable & Backward-Compatible: All new parameters MUST be added to config.yml, support environment variable overrides (using os.ExpandEnv()), and include sensible defaults to ensure existing config.yml files still work.

Testability: New features must be testable. Use interfaces (like PingWriter or StateManager) to allow for easy mocking in unit tests.

Secure by Default: All string data from external sources (SNMP, device responses) MUST be sanitized before being written to InfluxDB or logged.

⛔ Architectural Boundaries & Non-Goals
To keep the project focused, we explicitly do not do the following. Do not suggest code that:

Adds a Web UI: netscan is a headless backend service. A UI is out of scope.

Adds New Databases: Data persistence is exclusively for InfluxDB. Do not suggest adding support for Prometheus, MySQL, PostgreSQL, etc.

Performs Network Modification: This is a read-only monitoring tool. It must never perform active network changes (e.g., blocking IPs, modifying device configs).

Bypasses the StateManager: All device discovery and monitoring must be funneled through the central state.

Uses root for non-ICMP tasks: The root user in Docker is only for ICMP raw sockets. All other operations should be possible as a non-root user (even if the container runs as root).

Core Features (Implemented)
Configuration: Load parameters from config.yml with full environment variable support.

icmp_discovery_interval: How often to scan for new devices (e.g., "5m").

ping_interval: How often to ping known devices (e.g., "1s").

snmp_daily_schedule: What time to run the full SNMP scan (e.g., "02:00").

discovery_interval: Optional for backward compatibility (defaults to 4h if omitted).

State Management: A thread-safe, in-memory registry (StateManager) is the "single source of truth" for all known devices. It supports adding new devices, updating/enriching existing ones, and pruning stale devices that haven't responded to pings.

Decoupled ICMP Discovery: A dedicated scanner that runs on icmp_discovery_interval, sweeps network ranges, and feeds new IPs to the StateManager.

Decoupled SNMP Scanning: A dedicated, concurrent scanner that can be triggered in two ways:

On-demand (for a single, newly-discovered IP).

Scheduled (for all IPs currently in the StateManager).

Continuous Monitoring: A "reconciliation loop" that ensures every device in the StateManager has a dedicated, continuous pinger goroutine running. This loop also handles device removal, gracefully stopping the associated goroutine.

Resilience & Security (Mandatory):

All Fixes Integrated: All solutions from SECURITY_IMPROVEMENTS.md are implemented.

Timeouts: Aggressive context.WithTimeout applied to all external calls: ICMP ping, SNMP queries, and InfluxDB writes.

InfluxDB Protection: InfluxDB health check on startup, rate limiting on writes, and strict data sanitization/validation before every write.

Error Handling: A failed ping or SNMP query is logged but does not crash the pinger or the application.

Core Principles & Mandates (Read Before Coding)
These are the rules and best practices derived from production implementation. All new and existing code must follow them.

Docker & Deployment Mandates
Mandate: The container MUST run as root. This is a non-negotiable requirement for ICMP raw socket access in Linux containers. Do not attempt non-root workarounds for ICMP. This is an accepted security trade-off.

Mandate: Docker builds MUST be multi-stage.

Stage 1: Build with the correct Go version (golang:1.25-alpine).

Stage 2: Runtime with minimal alpine:latest.

Binaries MUST be stripped (-ldflags="-w -s") to keep the final image small (~15MB).

Mandate: Documentation MUST clearly separate Docker and Native deployment.

README.md is for Docker (primary).

README_NATIVE.md is for Native (alternative).

Security trade-offs (e.g., Docker root vs. Native setcap) MUST be explicitly explained in the docs.

Configuration Mandates
Mandate: All configuration MUST be loadable via environment variables. Use os.ExpandEnv() on the loaded config file. This is the standard for both Docker (docker-compose.yml or .env) and native (.env) deployments.

Mandate: Configuration changes MUST be backward-compatible. New fields must be optional and have sensible defaults. Do not break existing config.yml files.

Mandate: All example configurations (config.yml.example) MUST include prominent warnings for users to change default values (like network ranges) to match their environment.

SNMP Mandates
Mandate: All new SNMP queries MUST use snmpGetWithFallback(). Direct snmpGet calls are not permitted without this wrapper. This ensures compatibility with devices that fail on .0 OID instances by falling back to GetNext.

Mandate: All SNMP string processing MUST handle []byte (OctetString). Use a helper function (like validateSNMPString()) to check for both string and []byte types and convert as needed to prevent "invalid type" errors.

Logging & Data Mandates
Mandate: All new components MUST include diagnostic logging. At a minimum, log:

Configuration values being used (e.g., "Scanning networks: %v").

Entry/exit of major operations.

Specific error details with context (e.g., "Ping failed for %s: %v"). Do not silently fail.

Mandate: Summary logs MUST report both success and failure counts. (e.g., "Enriched X devices (failed: Y)").

Mandate: The InfluxDB schema MUST remain simple. Do not add fields that are not actively used for monitoring or queries (e.g., sysObjectID). The primary schema is device_info with IP, hostname, and snmp_description.

Mandate: Log all successful writes to InfluxDB. Include the device identifier (IP/hostname) in the log message for debugging and confirmation.

Technology Stack
Language: Go 1.25+ (updated from 1.21 - ensure Dockerfile uses golang:1.25-alpine)

Key Libraries:

gopkg.in/yaml.v3 (Config)

github.com/gosnmp/gosnmp (SNMP)

github.com/prometheus-community/pro-bing (ICMP)

github.com/influxdata/influxdb-client-go/v2 (InfluxDB)

sync.RWMutex (Critical for StateManager and activePingers map)

Deployment:

Docker + Docker Compose (primary deployment method)

InfluxDB v2.7 container

Alpine Linux base image

Testing & Validation Approach
Build & Test
Run go build ./cmd/netscan frequently during development

Run go test ./... to validate all tests pass

Run go test -race ./... to detect race conditions

All tests must pass before committing changes

Manual Validation
Test with actual config files to verify config loading

Test with diverse SNMP devices to verify compatibility

Monitor logs to ensure operations are working correctly

Verify InfluxDB writes by querying the database

CI/CD Requirements
./netscan --help must work (flag support required)

All unit tests must pass

Race detection must be clean: go test -race ./...

Build must succeed with no warnings

Docker Compose workflow validates full stack deployment

Workflow creates config.yml from template and runs docker compose up

📋 How to Add a New Feature (Example: New Scanner)
Follow this workflow when adding a new, recurring task:

Config: Add new parameters to internal/config/config.go and config.yml.example. (e.g., NewScannerInterval time.Duration). Ensure env var support and a sensible default.

Logic: Create the core logic in its own package (e.g., internal/newscanner/scanner.go). Adhere to all "Resilience First" principles.

State (If needed): Add thread-safe methods to internal/state/manager.go to store or retrieve any new data.

Database (If needed): Add a new method to internal/influx/writer.go to persist the new data (e.g., WriteNewData(...)). Remember to include timeouts, sanitization, and logging.

Orchestration: Add a new Ticker loop in cmd/netscan/main.go to run the new scanner at its configured interval. Ensure it's non-blocking and respects graceful shutdown.

Testing: Add unit tests for the new logic and validation rules. Use interfaces for mocking.

Documentation: Update this file and the README.md with the new feature.

Architecture & Implementation Details
Configuration (internal/config/config.go)
Implemented:

Config struct includes:

ICMPDiscoveryInterval time.Duration (e.g., "5m")

PingInterval time.Duration (e.g., "1s")

SNMPDailySchedule string (e.g., "02:00")

All resource limits from SECURITY_IMPROVEMENTS.md (max_devices, etc.)

DiscoveryInterval time.Duration (optional for backward compatibility, defaults to 4h)

Validation for SNMPDailySchedule (must be parseable as HH:MM, range 00:00 to 23:59)

Better error messages for invalid duration fields

State Manager (internal/state/manager.go)
Implemented:

Central hub, fully thread-safe with sync.RWMutex

Device struct stores: IP, Hostname, SysDescr, and LastSeen time.Time

Note: SysObjectID was removed as it's not needed for monitoring

Methods:

AddDevice(ip string) bool: Adds a device. Returns true if it was new.

UpdateDeviceSNMP(ip, hostname, sysDescr string): Enriches an existing device with SNMP data.

UpdateLastSeen(ip string): Called by pingers to keep a device alive.

GetAllIPs() []string: Returns all IPs for the daily SNMP scan.

PruneStale(olderThan time.Duration): Removes devices not seen recently.

Add(ip string): Legacy method maintained for compatibility.

Prune(olderThan time.Duration): Alias for PruneStale.

maxDevices limit and eviction logic fully implemented

InfluxDB Writer (internal/influx/writer.go)
Implemented:

All hardening from SECURITY_IMPROVEMENTS.md retained

NewWriter(...): Performs Health Check and returns an error on failure

WritePingResult(...): Uses short timeout (2s), logs errors, never panics

WriteDeviceInfo(ip, hostname, description string):

Uses longer timeout (5s)

Performs data sanitization on all string fields

Simplified signature (removed redundant snmp_name and snmp_sysid fields)

Client-side rate limiter is active

Schema simplified to essential fields only (IP, hostname, snmp_description)

Scanners (internal/discovery/scanner.go)
Implemented:

Two clear, concurrent functions for decoupled operations:

RunICMPSweep(networks []string, workers int) []string:

Uses a worker pool to ping all IPs in the CIDR ranges

Returns only the IPs that responded

CIDR expansion limits (e.g., max /16) from SECURITY_IMPROVEMENTS.md retained

RunSNMPScan(ips []string, snmpConfig *config.SNMPConfig, workers int) []state.Device:

Uses a worker pool to run SNMP queries against the provided list of IPs

Applies timeouts (config.Timeout) to each query

Gracefully handles hosts that don't respond to SNMP (logs error, continues)

Returns a list of state.Device structs with SNMP data filled in

SNMP Robustness Features:

snmpGetWithFallback() function: Tries Get first, falls back to GetNext if NoSuchInstance error occurs

Validates OID prefixes to ensure GetNext results are under requested base OID

Better compatibility with diverse device types and SNMP implementations

SNMP Type Handling:

validateSNMPString() handles both string and []byte types

Converts byte arrays (OctetString values) to strings for ASCII/UTF-8 encoded values

Prevents "invalid type: expected string, got []uint8" errors

Continuous Pinger (internal/monitoring/pinger.go)
Implemented:

StartPinger(ctx context.Context, wg *sync.WaitGroup, device state.Device, interval time.Duration, writer PingWriter, stateMgr StateManager):

Runs in its own goroutine for one device

Loops on a time.NewTicker(interval)

Inside the loop:

Perform a ping (with a short timeout)

Call writer.WritePingResult(...) with the outcome

If successful, call stateMgr.UpdateLastSeen(device.IP)

If ping or write fails, log the error and continue the loop (does not exit)

Listens for ctx.Done() to exit gracefully

Uses interface types (PingWriter, StateManager) for better testability

Properly tracks shutdown with WaitGroup

Orchestration (cmd/netscan/main.go)
Implemented - Multi-Ticker Architecture:

1. Initialization:

Load and validate config (with -config flag support)

Init StateManager

Init InfluxWriter with fail fast if health check fails

Setup signal handling for graceful shutdown (using a main context.Context)

activePingers := make(map[string]context.CancelFunc)

pingersMu sync.Mutex (CRITICAL: Used for all access to activePingers)

var pingerWg sync.WaitGroup for tracking all pinger goroutines

2. Ticker 1: ICMP Discovery Loop (Runs every config.ICMPDiscoveryInterval, e.g., 5m):

Logs: "Starting ICMP discovery scan..."

responsiveIPs := discovery.RunICMPSweep(...)

For each responsiveIP:

isNew := stateMgr.AddDevice(ip)

If isNew:

Logs: "New device found: %s. Performing initial SNMP scan."

Triggers immediate, non-blocking scan in goroutine

SNMP scan results update StateManager and write to InfluxDB

Logs success or failure for debugging

3. Ticker 2: Daily SNMP Scan Loop (Runs at config.SNMPDailySchedule, e.g., "02:00"):

Calculates next run time using time parsing

Logs: "Starting daily full SNMP scan..."

allIPs := stateMgr.GetAllIPs()

snmpDevices := discovery.RunSNMPScan(allIPs, ...)

Updates StateManager with SNMP data

Writes device info to InfluxDB

Logs: "Daily SNMP scan complete."

Enhanced logging: Shows success/failure counts, confirms InfluxDB writes

4. Ticker 3: Pinger Reconciliation Loop (Runs every 5 seconds):

Ensures activePingers map matches StateManager

pingersMu.Lock() (Lock for entire reconciliation)

currentStateIPs := stateMgr.GetAllIPs() converted to set

Start new pingers:

For each IP in state not in activePingers:

Logs: "Starting continuous pinger for %s"

Creates pingerCtx, pingerCancel := context.WithCancel(mainCtx)

activePingers[ip] = pingerCancel

pingerWg.Add(1)

go monitoring.StartPinger(pingerCtx, &pingerWg, ...)

Stop old pingers:

For each IP in activePingers not in state:

Logs: "Stopping continuous pinger for stale device %s"

Calls cancelFunc()

delete(activePingers, ip)

pingersMu.Unlock()

5. Ticker 4: State Pruning Loop (Runs every 1 hour):

Logs: "Pruning stale devices..."

stateMgr.PruneStale(24 * time.Hour)

6. Graceful Shutdown:

When mainCtx is canceled (by SIGINT/SIGTERM):

Stop all tickers

pingersMu.Lock(), iterate activePingers, call cancelFunc() for all

pingersMu.Unlock()

pingerWg.Wait() to wait for all pingers to exit

Close InfluxDB client

Logs: "Shutdown complete."

Key Implementation Notes:

All four tickers run concurrently and independently

Pinger reconciliation ensures consistency between state and active pingers

Proper mutex protection prevents race conditions on activePingers map

WaitGroup ensures clean shutdown of all goroutines

Enhanced logging provides visibility into all operations
