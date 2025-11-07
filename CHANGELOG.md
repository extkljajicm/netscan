# Changelog

All notable changes to netscan will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

- **Critical bug in suspended_devices counter**: Fixed permanent counter inflation in health metrics
  - **Bug**: `Add()` method failed to decrement `suspended_devices` counter when updating devices with expired suspensions
  - **Root cause**: State transition logic only checked for ACTIVE suspensions (SuspendedUntil in future), ignoring expired suspensions
  - **Impact**: Counter could become permanently inflated when devices were updated after suspension expired but before cleanup ran
  - **Fix**: `Add()` now cleans up expired suspensions in existing device before state transition checks, ensuring counter stays accurate
  - **Test coverage**: Added `TestBugFixed_ExpiredSuspensionNowDecremented` to prevent regression
  - **Files changed**: `internal/state/manager.go` (lines 89-108)
  - **Documentation**: Added comprehensive audit report in `AUDIT_REPORT.md`

### Added

- **Docker log rotation**: Configured automatic log rotation for netscan service in docker-compose.yml
  - Prevents indefinite log file growth and disk space exhaustion
  - Max log file size: 10MB per file
  - Retains 3 most recent log files (~30MB total disk usage)
  - Uses Docker's json-file logging driver with rotation options
- **Automatic health bucket creation**: InfluxDB initialization script for dual-bucket architecture
  - New `init-influxdb.sh` script automatically creates "health" bucket on container startup
  - Mounted to `/docker-entrypoint-initdb.d/` for automatic execution
  - Waits for InfluxDB to be ready before bucket creation
  - Uses same retention period as main bucket (default: 1w)
  - Fixes "bucket 'health' not found" errors when writing health metrics
  - Documentation updated in config.yml.example to include `health_bucket` field

### Changed

- **Performance optimization for high-resource hardware**: Updated default configuration values to support large-scale deployments
  - `icmp_workers`: 64 → 1024 (16x increase for faster discovery scans on high-performance servers)
  - `snmp_workers`: 32 → 256 (8x increase for better concurrent SNMP polling)
  - `max_devices`: 10000 → 20000 (2x increase to support larger networks)
  - `max_concurrent_pingers`: 1000 → 20000 (20x increase to match device capacity)
  - `memory_limit_mb`: 512 → 16384 (32x increase for 16GB RAM servers)
  - `influxdb.batch_size`: 100 → 5000 (50x increase for better write throughput)
  - Updated validation limits: `icmp_workers` max 2000, `snmp_workers` max 1000, `max_devices` max 100000, `max_concurrent_pingers` max 100000, `memory_limit_mb` max 16384
  - **Rationale**: Balanced configuration optimizes for high-performance servers (8+ cores, 16GB+ RAM) while controlling ICMP packet rates
  - **Backward compatibility**: Existing config files work unchanged; values remain fully configurable for smaller deployments
  - Documentation updated in README.md and MANUAL.md with deployment size guidelines

### Added

- Health metrics persistence: New decoupled service to periodically write application health metrics to separate InfluxDB bucket
  - New configuration parameters: `influxdb.health_bucket` (default: "health") and `health_report_interval` (default: "10s")
  - New Ticker 5: Health Report Loop writes metrics every 10 seconds by default
  - Dual-bucket architecture in InfluxDB writer with separate WriteAPI for health metrics
  - New measurement `health_metrics` with fields: device_count, active_pingers, goroutines, memory_mb, influxdb_ok, influxdb_successful_batches, influxdb_failed_batches
  - Refactored health server to expose public `GetHealthMetrics()` method for reusability
  - Full backward compatibility: existing config files work without changes (defaults applied)

### Summary

netscan is a Go network monitoring service (Go 1.25+) targeting linux-amd64 exclusively. The application implements a decoupled, multi-ticker architecture for efficient network device discovery and continuous monitoring.

### Core Features

**Multi-Ticker Architecture**
- Five independent concurrent event loops orchestrated in main.go
- ICMP Discovery Ticker: Finds new devices via ping sweeps (configurable interval, default 5m)
- Daily SNMP Scan Ticker: Enriches all devices with metadata at scheduled time (e.g., 02:00)
- Pinger Reconciliation Ticker: Ensures all devices have active monitoring goroutines (every 5s)
- State Pruning Ticker: Removes stale devices (every 1h, 24h threshold)
- Health Report Ticker: Writes health metrics to InfluxDB (configurable interval, default 10s)

**Discovery & Monitoring**
- Concurrent ICMP discovery with configurable worker pools (default: 64 workers)
- Concurrent SNMP scanning with worker pools (default: 32 workers)
- Continuous per-device ping monitoring (dedicated goroutine per device, default: 2s interval)
- Dual-trigger SNMP: Immediate scan on discovery + scheduled daily scan for all devices
- SNMP robustness: GetNext fallback for device compatibility, handles both string and []byte OctetString types

**State Management**
- Thread-safe StateManager as single source of truth for all devices
- Device struct: IP, Hostname, SysDescr, LastSeen timestamp
- LRU eviction when max_devices limit reached (default: 10000)
- RWMutex protection for all operations
- Device metadata enrichment via SNMP

**Data Persistence**
- InfluxDB v2 time-series metrics storage
- Batching system (default: 100 points per batch, 5s flush interval)
- Background flusher goroutine with graceful shutdown
- Data sanitization and validation before writes
- Health check on startup (fail-fast if InfluxDB unavailable)

**Observability & Monitoring**
- HTTP health check server (default port: 8080)
- Three endpoints: /health (detailed JSON), /health/ready (readiness), /health/live (liveness)
- Docker HEALTHCHECK directive integration
- Health metrics: device count, active pingers, InfluxDB stats, memory, goroutines, uptime

**Logging**
- Structured logging with zerolog
- Machine-parseable JSON logs
- Colored console output for development
- Context-rich logging: IP addresses, device counts, durations, errors
- Log levels: Fatal, Error, Warn, Info, Debug

**Testing & Quality**
- Comprehensive test suite covering orchestration logic
- Race detection clean: all tests pass with -race flag
- Unit tests for all packages
- Performance benchmarks for regression detection

**Configuration**
- YAML configuration file with environment variable expansion (${VAR_NAME} syntax)
- CLI flag support: -config to specify custom config path
- Backward compatibility: optional discovery_interval field with sensible default
- Comprehensive validation on startup with clear error messages
- Resource protection: limits for devices, pingers, memory, scan intervals

**Deployment Options**
- Docker deployment (recommended): Multi-stage Dockerfile, ~15MB Alpine image, docker-compose.yml
- Native systemd deployment (alternative): Non-root service user, CAP_NET_RAW via setcap
- Automated deployment scripts: deploy.sh (install), undeploy.sh (cleanup)
- Docker verification script for CI/CD

**Security**
- IP address validation: prevents loopback, multicast, link-local, unspecified addresses
- SNMP string sanitization: removes null bytes, control characters, validates UTF-8
- Network range validation: prevents dangerous scans
- Docker: Runs as root (required for ICMP), container isolation provides security boundary
- Native: Runs as non-root service user with Linux capabilities
- Security scanning in CI/CD: govulncheck for Go, Trivy for Docker images

**Performance Optimizations**
- Lock-free batching via buffered channels
- Worker pool patterns for concurrent operations
- Streaming CIDR expansion (no intermediate arrays)
- Context-based cancellation for clean shutdown
- WaitGroup tracking for all goroutines
- Panic recovery on all goroutines

### Technology Stack

- **Language:** Go 1.25.1
- **Platform:** linux-amd64 (exclusively, multi-architecture deferred)
- **Dependencies:**
  - gopkg.in/yaml.v3 v3.0.1 (configuration)
  - github.com/gosnmp/gosnmp v1.42.1 (SNMP)
  - github.com/prometheus-community/pro-bing v0.7.0 (ICMP)
  - github.com/influxdata/influxdb-client-go/v2 v2.14.0 (InfluxDB)
  - github.com/rs/zerolog v1.34.0 (logging)
- **Infrastructure:** Docker + Docker Compose, InfluxDB v2.7, Alpine Linux

### Project Structure

```
netscan/
├── cmd/netscan/              # Main application
│   ├── main.go              # Multi-ticker orchestration
│   ├── health.go            # Health check HTTP server
│   └── orchestration_test.go # Integration tests
├── internal/
│   ├── config/              # YAML parsing, validation
│   ├── discovery/           # ICMP/SNMP scanning
│   ├── influx/              # InfluxDB batched writes
│   ├── logger/              # Structured logging
│   ├── monitoring/          # Continuous ping monitoring
│   └── state/               # Thread-safe device registry
├── .github/
│   ├── copilot-instructions.md # Comprehensive dev guide
│   └── workflows/ci-cd.yml  # Security scanning, tests
├── docker-compose.yml       # Docker stack definition
├── Dockerfile               # Multi-stage build
├── config.yml.example       # Configuration template
├── .env.example             # Environment variables
├── deploy.sh / undeploy.sh  # Native deployment
└── README.md / MANUAL.md    # Documentation
```

### CI/CD Pipeline

- Go build validation (linux-amd64)
- Full test suite with race detection
- Security scanning: govulncheck (Go dependencies), Trivy (filesystem + Docker)
- SARIF report upload to GitHub Security tab
- Docker Compose workflow validation
- Blocks deployment on HIGH/CRITICAL vulnerabilities

### Future Work (Deferred)

Complex scalability and platform features intentionally deferred for future releases:
- Rate limiting & circuit breakers for failing devices
- SNMP connection pooling
- Enhanced context propagation with distributed tracing
- Multi-architecture support (ARM64, ARM v7)
- SNMPv3 with authentication and encryption
- IPv6 support
- State persistence to disk
- Device grouping & tagging
- Webhook alerting
- Prometheus metrics export

### Breaking Changes

None - This is the initial documented release state.

### Upgrade Notes

Not applicable - This changelog documents the current implementation state.

