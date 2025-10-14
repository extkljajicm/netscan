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

### Security
- **Phase 1 - Configuration Security**: Environment variable support with secure .env files
- **Phase 2 - Input Validation**: Comprehensive validation and sanitization across all inputs
- **Phase 3 - Resource Protection**: Rate limiting, memory bounds, and DoS prevention
  - Discovery scan rate limiting with configurable minimum intervals (`min_scan_interval`)
  - Concurrent pinger limits to prevent goroutine exhaustion (`max_concurrent_pingers`)
  - Device count limits with automatic cleanup of oldest devices (`max_devices`)
  - InfluxDB write rate limiting (100 writes/second)
  - Memory usage monitoring with configurable limits (`memory_limit_mb`)
  - Buffer size restrictions and resource bounds
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
- **CI/CD Pipeline**: GitHub Actions workflow for automated testing, building, and releases
  - Multi-platform binary builds (Linux/macOS/Windows, AMD64/ARM64)
  - Automated changelog generation with git-cliff
  - Release automation with version tagging
  - Code coverage reporting
- **Documentation**: Comprehensive README rewrite with technical specifications
  - Complete development workflow documentation
  - Performance tuning guidelines
  - Troubleshooting and deployment guides
  - Multi-network scanning support examples
- **Environment Variable Support**: Sensitive configuration values (InfluxDB tokens, SNMP community strings) can now use environment variables with `${VAR_NAME}` syntax for secure credential management
- **Secure .env File**: Deployment script now creates a separate `.env` file with restrictive permissions (600) for sensitive credentials, following 12-factor app principles
- **Test Environment Integration**: .env file defaults match docker-compose.yml values for seamless testing without configuration changes
- **Configuration Validation**: Comprehensive input validation at startup including network range validation, bounds checking, and required field verification
- **Security Hardening**: Prevents scanning dangerous network ranges (loopback, multicast, link-local, overly broad CIDR blocks)
- **Runtime Input Validation**: Added validation for SNMP responses, IP addresses, and InfluxDB data to prevent injection and corruption
- **SNMP Security**: Enhanced SNMP community string validation with weak password detection and character restrictions
- **URL Validation**: InfluxDB URL validation with scheme and format checking
- **Data Sanitization**: String sanitization for SNMP responses and InfluxDB fields to prevent corruption

### Fixed
- **Deployment Configuration**: deploy.sh now properly copies config.yml.example as config.yml template instead of using incorrect local config files
- **Systemd Service Security**: Resolved ICMP blocking issue by implementing AmbientCapabilities=CAP_NET_RAW while maintaining NoNewPrivileges=yes for proper security hardening
- **Service Permissions**: Corrected systemd security settings to enable network discovery operations without compromising system security
- **Configuration Security**: Resolved plaintext credential exposure by implementing environment variable support for sensitive values
- **Input Validation**: Added comprehensive validation to prevent dangerous configurations and resource exhaustion attacks

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

### Security
- **Production Deployment**: Documented requirement to update default test credentials in .env file before production deployment
- **Credential Security**: Enhanced guidance for secure credential management across environments

