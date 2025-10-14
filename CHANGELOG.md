# Changelog

All notable changes to netscan will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased] - 2025-10-14

### Added
- **Concurrent Scanning**: 64-worker goroutine pools for high-performance network discovery
- **Continuous Monitoring**: Real-time ICMP ping monitoring with configurable intervals
- **InfluxDB Integration**: Time-series metrics storage with InfluxDB v2 client
- **Thread-Safe State Management**: RWMutex-protected device state with automatic pruning
- **Graceful Shutdown**: Signal handling (SIGINT/SIGTERM) with proper resource cleanup
- **Configuration Management**: YAML-based configuration with duration parsing
- **Deployment Scripts**: Automated systemd service installation and management
- **Docker Testing**: docker-compose setup for local InfluxDB testing environment
- **Comprehensive Testing**: Unit tests covering all major components
- **Build Automation**: Cross-platform build scripts and deployment tools
- **Configurable Worker Counts**: ICMP and SNMP worker pools now configurable via `icmp_workers` and `snmp_workers` settings
  - Default ICMP workers: 64
  - Default SNMP workers: 32
  - Allows performance tuning based on system capabilities
- **Enhanced Metrics**: SNMP data now included in InfluxDB metrics as additional fields
  - `snmp_name`: Device hostname/SNMP sysName
  - `snmp_description`: SNMP sysDescr MIB-II value
  - `snmp_sysid`: SNMP sysObjectID MIB-II value
  - Maintains existing tags (ip, hostname) and fields (rtt_ms, success)
- **Optimized Metrics Storage**: Separated ping metrics from device metadata for better cardinality
  - `ping` measurement: IP tag only, rtt/success fields (high-frequency)
  - `device_info` measurement: Device metadata stored once per discovery (low-frequency)
  - Eliminates redundant SNMP data storage on every ping measurement

### Dependencies
- `github.com/gosnmp/gosnmp v1.42.1`
- `github.com/influxdata/influxdb-client-go/v2 v2.14.0`
- `github.com/prometheus-community/pro-bing v0.7.0`
- `gopkg.in/yaml.v3 v3.0.1`

### Technical Implementation Details
- **Two-Phase Discovery**: ICMP ping sweep first (64 workers), then SNMP polling only on online devices (32 workers)
  - Eliminates SNMP timeouts on offline devices
  - Faster initial network mapping
  - Graceful fallback for SNMP-unavailable devices
- **Concurrency Pattern**: Producer-consumer model with buffered channels
  - Job channel for IP addresses, results channel for discovered devices
  - Context-based cancellation for graceful shutdown
- **Error Handling**: Multi-layer error propagation with logging
  - ICMP packet loss handling, SNMP timeout/retry logic
  - InfluxDB write failures with exponential backoff
- **Resource Management**: Proper goroutine lifecycle with sync.WaitGroup
  - Ticker-based scheduling for periodic operations
  - Signal handling for SIGINT/SIGTERM with cleanup
- **Security Model**: Linux capabilities for privilege separation
  - CAP_NET_RAW for raw socket access without full root privileges
  - Dedicated service user isolation

### Known Issues
- IPv4-only implementation
- SNMPv2c community string authentication only
- Requires CAP_NET_RAW capability for ICMP operations
- No built-in alerting or notification system

### Future Work
- IPv6 support
- SNMPv3 authentication
- Additional discovery protocols (LLDP, CDP)
- Web dashboard
- Alerting integration
- Plugin architecture

