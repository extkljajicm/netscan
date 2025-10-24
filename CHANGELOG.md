# Changelog

All notable changes to netscan will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased] - 2025-10-24

### Added
- **Health Check Endpoint**: HTTP server with three endpoints for production monitoring
  - `/health`: Detailed JSON status with device count, uptime, memory, goroutines, and InfluxDB connectivity
  - `/health/ready`: Readiness probe (returns 200 if InfluxDB OK, 503 if unavailable)
  - `/health/live`: Liveness probe (returns 200 if application running)
  - Docker HEALTHCHECK directive integration
  - Kubernetes probe support for orchestrated deployments
  - Configurable port via `health_check_port` (default: 8080)
- **Batch InfluxDB Writes**: Performance-optimized batching system
  - Points accumulated in memory up to configurable batch size (default: 100)
  - Automatic flush when batch full or on timer interval (default: 5s)
  - Background flusher goroutine with graceful shutdown
  - 99% reduction in InfluxDB requests for large deployments
  - Configuration fields: `batch_size` and `flush_interval`
- **Structured Logging**: Machine-parseable JSON logs with zerolog
  - Context-rich logging with IP addresses, device counts, error details, durations
  - Production JSON format for log aggregation (ELK, Splunk, etc.)
  - Development-friendly colored console output
  - Zero-allocation performance
  - Configurable log levels (Fatal, Error, Warn, Info, Debug)
  - New package: `internal/logger`
- **Security Scanning**: Automated vulnerability detection in CI/CD
  - govulncheck for Go dependency scanning
  - Trivy filesystem scan for secrets and misconfigurations
  - Trivy Docker image scan for OS and library vulnerabilities
  - GitHub Security integration with SARIF uploads
  - Blocks deployment on CRITICAL/HIGH vulnerabilities
- **Comprehensive Orchestration Tests**: Test suite for critical ticker management code
  - 11 test functions covering ticker lifecycle, shutdown, and reconciliation
  - Performance benchmark with 1000 devices for regression detection
  - Race detection clean
  - 527 lines of production-ready tests in `cmd/netscan/orchestration_test.go`
- **Dependencies**: 
  - `github.com/rs/zerolog v1.34.0` for structured logging

### Changed
- **InfluxDB Writer**: Switched from blocking to non-blocking WriteAPI for batching support
- **Main Application**: Integrated health server startup and structured logging initialization
- **Docker Configuration**: Added EXPOSE 8080 and HEALTHCHECK directive
- **Logging**: Replaced all standard library logging with structured zerolog logging
- **Documentation**: Consolidated all analysis files into `.github/copilot-instructions.md`

### Removed
- **Temporary Analysis Files**: Removed 6 analysis documents (ANALYSIS_SUMMARY.md, COPILOT_IMPROVEMENTS.md, COPILOT_INSTRUCTIONS_UPDATE.md, IMPLEMENTATION_GUIDE.md, IMPROVEMENTS_README.md, REFACTORING_SUMMARY.md)

## [Unreleased] - 2025-10-18

### Added
- **Multi-Ticker Architecture**: Four independent concurrent tickers for decoupled operations
  - ICMP Discovery Ticker: Finds new devices (configurable interval)
  - Daily SNMP Scan Ticker: Enriches devices at scheduled time
  - Pinger Reconciliation Ticker: Ensures all devices monitored
  - State Pruning Ticker: Removes stale devices (24h)
- **Dual-Trigger SNMP**: Immediate scan for new devices + scheduled daily scan for all devices
- **State-Centric Design**: StateManager as single source of truth with thread-safe operations
- **Configuration**: `snmp_daily_schedule` field for scheduling daily SNMP scans (HH:MM format)
- **CLI**: `-config` flag for specifying custom config file path
- **SNMP Robustness**: GetNext fallback for devices without standard .0 OID instances
- **SNMP Type Handling**: Support for both string and byte array OctetString values

### Changed
- **InfluxDB Schema**: Simplified device_info measurement with only essential fields (IP, hostname, snmp_description)
- **Configuration**: Made `discovery_interval` optional for backward compatibility

### Removed
- **CLI**: `--icmp-only` flag (no longer needed with multi-ticker architecture)

## [Unreleased] - 2025-10-14

### Added
- Concurrent ICMP/SNMP scanning with configurable worker pools
- Continuous ICMP ping monitoring with InfluxDB metrics storage
- Thread-safe device state management with automatic pruning
- YAML configuration with environment variable support for sensitive values
- Automated deployment scripts (deploy.sh, undeploy.sh)
- Docker Compose test environment with InfluxDB
- Comprehensive unit tests with race detection
- GitHub Actions CI/CD pipeline

### Security
- Environment variable support for credentials (secure .env files)
- Comprehensive input validation and sanitization
- Resource protection: rate limiting, memory bounds, goroutine limits
- Network range validation (prevents dangerous scans)
- CAP_NET_RAW capability with systemd hardening

### Dependencies
- `github.com/gosnmp/gosnmp v1.42.1`
- `github.com/influxdata/influxdb-client-go/v2 v2.14.0`
- `github.com/prometheus-community/pro-bing v0.7.0`
- `gopkg.in/yaml.v3 v3.0.1`

