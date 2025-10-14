GitHub Copilot Instructions for Project: netscan

## Project Goal
Create a robust, long-running network monitoring service in Go with comprehensive security. The service performs periodic SNMP discovery on large network ranges to find active devices. For each discovered device, it initiates continuous ICMP pinging and writes performance metrics to an InfluxDB time-series database. The final output is a single executable file for easy deployment.

## Core Features
- **Configuration**: Load all parameters from config.yml with environment variable support
- **Periodic Discovery**: Scan defined CIDR ranges using high-concurrency worker pools to find devices via SNMPv2c
- **State Management**: Thread-safe in-memory device registry with automatic lifecycle management
- **Continuous Monitoring**: Dedicated goroutines for ICMP ping monitoring with configurable intervals
- **Data Persistence**: Write ping results to InfluxDB with optimized dual-measurement strategy
- **Security**: Comprehensive input validation, resource protection, and secure credential management
- **Deployment**: Automated systemd service installation with capability management

## Technology Stack
**Language**: Go 1.21+

**Key Libraries**:
- `gopkg.in/yaml.v3` - YAML configuration parsing
- `github.com/gosnmp/gosnmp` - SNMPv2c protocol implementation
- `github.com/prometheus-community/pro-bing` - ICMP ping functionality
- `github.com/influxdata/influxdb-client-go/v2` - InfluxDB v2 client

## Security Architecture
**Phase 1 - Configuration Security**: Environment variable expansion with secure .env files
**Phase 2 - Input Validation**: Comprehensive validation and sanitization across all inputs
**Phase 3 - Resource Protection**: Rate limiting, memory bounds, and DoS prevention
- Discovery scan rate limiting with configurable minimum intervals
- Concurrent pinger limits to prevent goroutine exhaustion
- Device count limits with automatic cleanup
- InfluxDB write rate limiting (100 writes/second)
- Memory usage monitoring with configurable limits

## Current Project Structure
```
/home/marko/Projects/netscan/
├── cmd/netscan/
│   ├── main.go           # Service orchestration, signal handling, discovery loops
│   └── main_test.go      # Basic package tests
├── internal/
│   ├── config/
│   │   ├── config.go     # YAML parsing, environment expansion, validation
│   │   └── config_test.go # Configuration parsing tests
│   ├── discovery/
│   │   ├── scanner.go    # ICMP/SNMP concurrent scanning workers
│   │   └── scanner_test.go # Discovery algorithm tests
│   ├── influx/
│   │   ├── writer.go     # InfluxDB client with rate limiting and validation
│   │   └── writer_test.go # Database write operation tests
│   ├── monitoring/
│   │   ├── pinger.go     # ICMP ping goroutines with metrics collection
│   │   └── pinger_test.go # Ping monitoring tests
│   └── state/
│       ├── manager.go    # Thread-safe device registry with resource limits
│       └── manager_test.go # State management concurrency tests
├── .github/
│   ├── copilot-instructions.md  # This file
│   └── workflows/
│       └── ci-cd.yml       # GitHub Actions pipeline
├── docker-compose.yml      # Test InfluxDB environment
├── build.sh               # Simple binary build script
├── deploy.sh              # Production deployment automation
├── undeploy.sh            # Complete uninstallation script
├── config.yml.example     # Configuration template
├── cliff.toml            # Changelog generation configuration
├── go.mod
├── go.sum
├── README.md
├── CHANGELOG.md
└── LICENSE.md
```

## Step-by-Step Implementation Plan

### Step 1: Initialize Project and Dependencies
Initialize the Go module: `go mod init github.com/extkljajicm/netscan`.

Add the required dependencies:
```bash
go get gopkg.in/yaml.v3
go get github.com/gosnmp/gosnmp
go get github.com/prometheus-community/pro-bing
go get github.com/influxdata/influxdb-client-go/v2
```

Create the config.yml.example file with the following structure:

```yaml
# config.yml.example - netscan network monitoring configuration
#
# SECURITY NOTE: Sensitive credentials are loaded from a separate .env file
# for enhanced security. The .env file is created during deployment with
# restrictive permissions (600) and contains default test values that match
# docker-compose.yml. For production use, update the .env file with secure
# credentials.
#
# Required environment variables in .env file:
# - INFLUXDB_TOKEN=netscan-token (default for testing)
# - INFLUXDB_ORG=test-org (default for testing)
# - SNMP_COMMUNITY=public (default for testing - change for production security)
#
# DO NOT store actual credentials in this config.yml file!

# =============================================================================
# DISCOVERY SETTINGS
# =============================================================================
# How often to run the full SNMP discovery scan (ICMP sweep + SNMP polling)
discovery_interval: "4h"

# How often to run ICMP discovery in --icmp-only mode
icmp_discovery_interval: "5m"

# Network ranges to scan (CIDR notation) - supports multiple subnets
networks:
  - "192.168.0.0/24"
  - "10.0.0.0/16"
  - "172.16.0.0/12"

# =============================================================================
# PERFORMANCE TUNING
# =============================================================================
# Number of concurrent ICMP ping workers (recommended: 2-4x CPU cores)
icmp_workers: 64

# Number of concurrent SNMP polling workers (recommended: 1-2x CPU cores)
snmp_workers: 32

# =============================================================================
# MONITORING SETTINGS
# =============================================================================
# Ping frequency per monitored device
ping_interval: "2s"

# Timeout for individual ping operations
ping_timeout: "2s"

# =============================================================================
# SNMP SETTINGS
# =============================================================================
# SNMPv2c community string for device authentication
# Sensitive values are loaded from .env file (see Environment Variables section)
snmp:
  community: "${SNMP_COMMUNITY}"  # Loaded from .env file - default is 'public' for testing
  port: 161
  timeout: "5s"
  retries: 1

# =============================================================================
# INFLUXDB SETTINGS
# =============================================================================
# Time-series database for metrics storage
# Sensitive values are loaded from .env file (see Environment Variables section)
influxdb:
  url: "http://localhost:8086"
  token: "${INFLUXDB_TOKEN}"  # Loaded from .env file
  org: "${INFLUXDB_ORG}"      # Loaded from .env file
  bucket: "netscan"

# =============================================================================
# RESOURCE PROTECTION SETTINGS
# =============================================================================
# Limits to prevent resource exhaustion and DoS attacks
max_concurrent_pingers: 1000  # Maximum number of concurrent ping goroutines
max_devices: 10000            # Maximum number of devices to monitor
min_scan_interval: "1m"       # Minimum interval between discovery scans
memory_limit_mb: 512          # Memory usage limit in MB
```

### Step 2: Implement Configuration (internal/config/config.go)
Create a Go struct that mirrors the config.yml structure with environment variable expansion. Implement:
- `LoadConfig(path string) (*Config, error)` - reads and parses YAML with env var support
- `ValidateConfig(cfg *Config) (string, error)` - validates configuration and security constraints
- Use `time.ParseDuration` for interval strings and `os.ExpandEnv` for variable expansion

### Step 3: Implement the State Manager (internal/state/manager.go)
Thread-safe device registry with resource limits:

Define `Device` struct: `{ IP, Hostname, SysDescr, SysObjectID string }`

Create `Manager` struct with:
- `devices map[string]*Device` with `sync.RWMutex`
- `maxDevices int` parameter for resource protection

Implement methods:
- `NewManager(maxDevices int) *Manager`
- `Add(device Device)` - with device count limits
- `Get(ip string) (*Device, bool)`
- `GetAll() []Device`
- `UpdateLastSeen(ip string)`
- `Prune(olderThan time.Duration)` - removes stale devices

### Step 4: Implement InfluxDB Writer (internal/influx/writer.go)
InfluxDB client with rate limiting and validation:

Create `Writer` struct with:
- InfluxDB client and write API
- Rate limiter (100 writes/second)
- Input validation and sanitization

Implement methods:
- `NewWriter(url, token, org, bucket string) *Writer`
- `WriteDeviceInfo(ip, hostname, sysName, sysDescr, sysObjectID string)` - metadata writes
- `WritePingResult(ip string, rtt time.Duration, successful bool)` - metrics writes
- `validateIPAddress(ip string) error` - security validation
- `sanitizeInfluxString(s, fieldName string) string` - data sanitization

### Step 5: Implement the Pinger (internal/monitoring/pinger.go)
ICMP monitoring with resource management:

Create `StartPinger(device state.Device, config *config.Config, writer *influx.Writer, ctx context.Context)`:
- Dedicated goroutine per device
- `time.NewTicker` based on `ping_interval`
- Uses `github.com/prometheus-community/pro-bing` for ICMP
- Calls `writer.WritePingResult()` with outcomes
- Respects `max_concurrent_pingers` limit
- Graceful shutdown on context cancellation

### Step 6: Implement the Discovery Scanner (internal/discovery/scanner.go)
Concurrent network scanning with security validation:

Create `RunFullDiscovery(config *config.Config) []state.Device`:
- Concurrent worker pool pattern (64 ICMP + 32 SNMP workers)
- Channels for jobs and results
- Producer generates IPs from CIDR ranges
- Workers perform SNMP Get requests for `sysName`, `sysDescr`, `sysObjectID`
- Input validation and rate limiting
- Returns discovered devices slice

Create `RunPingDiscovery(network string, workers int) []state.Device`:
- ICMP-only discovery for `--icmp-only` mode
- Concurrent ping sweeps
- Returns online devices for basic connectivity monitoring

### Step 7: Orchestrate in main.go (cmd/netscan/main.go)
Service orchestration with comprehensive security:

Main function responsibilities:
- Load and validate configuration
- Initialize state manager and InfluxDB writer
- Set up signal handling (SIGINT/SIGTERM)
- Create `activePingers map[string]context.CancelFunc` for goroutine management
- Implement dual discovery modes:
  - Full mode: ICMP sweep + SNMP polling on `discovery_interval`
  - ICMP-only mode: Ping sweeps only on `icmp_discovery_interval`
- Resource monitoring: `checkMemoryUsage()` function
- Rate limiting: `min_scan_interval` enforcement
- Device lifecycle: Start pingers for new devices, prune offline devices
- Graceful shutdown: Cancel all pingers and cleanup resources