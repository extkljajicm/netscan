# Changelog

All notable changes to netscan will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
- **Documentation**: `REFACTORING_SUMMARY.md` and `MIGRATION_GUIDE.md` for migration details

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

