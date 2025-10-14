# netscan

Network monitoring service written in Go that performs ICMP ping monitoring of discovered devices and stores metrics in InfluxDB.

## Overview

Performs two-phase network discovery: ICMP ping sweeps followed by SNMP polling of online devices. Continuously monitors device availability and latency.

## Features

- **Two-Phase Discovery**: ICMP sweep (configurable workers) then SNMP polling (configurable workers) on online devices
- **Dual Modes**: Full discovery (ICMP + SNMP) or ICMP-only mode
- **Concurrent Processing**: Configurable worker pool patterns for scalable network operations
- **State Management**: RWMutex-protected device state with timestamp-based pruning
- **InfluxDB v2**: Time-series metrics storage with point-based writes
- **Configuration**: YAML-based config with duration parsing
- **Security**: Linux capabilities (CAP_NET_RAW) for non-root ICMP access
- **Single Binary**: No runtime dependencies

## Architecture

```
cmd/netscan/main.go           # Orchestration and CLI interface
internal/
├── config/config.go          # YAML parsing with duration conversion
├── discovery/scanner.go      # ICMP/SNMP discovery with worker pools
├── monitoring/pinger.go      # ICMP monitoring goroutines
├── state/manager.go          # Thread-safe device state (RWMutex)
└── influx/writer.go          # InfluxDB client wrapper
```

## Dependencies

- `github.com/gosnmp/gosnmp v1.42.1` - SNMPv2c protocol
- `github.com/influxdata/influxdb-client-go/v2 v2.14.0` - InfluxDB v2 client
- `github.com/prometheus-community/pro-bing v0.7.0` - ICMP ping library
- `gopkg.in/yaml.v3 v3.0.1` - YAML configuration parser

## Installation

### Prerequisites
- Go 1.21+ (tested with 1.25.1)
- InfluxDB 2.x
- Root privileges for ICMP socket access

### Ubuntu
```bash
sudo apt update
sudo apt install golang-go docker.io docker-compose
sudo systemctl enable docker
sudo systemctl start docker
```

### CachyOS
```bash
sudo pacman -S go docker docker-compose
sudo systemctl enable docker
sudo systemctl start docker
```

### Setup
```bash
git clone https://github.com/extkljajicm/netscan.git
cd netscan
go mod download
sudo docker-compose up -d  # Start test InfluxDB
```

## Building

```bash
go build -o netscan ./cmd/netscan
# Or use build script
./build.sh
```

## Testing Deployment

The repository includes both deployment and undeployment scripts for safe testing:

```bash
# Deploy netscan as a systemd service
sudo ./deploy.sh

# Completely uninstall and clean up (for testing)
sudo ./undeploy.sh
```

The undeployment script provides a 100% clean removal of all components installed by `deploy.sh`.

## Configuration

Copy and edit configuration:

```bash
cp config.yml.example config.yml
```

### Configuration Structure

```yaml
# config.yml.example - netscan network monitoring configuration

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
snmp:
  community: "public"
  port: 161
  timeout: "5s"
  retries: 1

# =============================================================================
# INFLUXDB SETTINGS
# =============================================================================
# Time-series database for metrics storage
influxdb:
  url: "http://localhost:8086"
  token: "netscan-token"
  org: "test-org"
  bucket: "netscan"
```

### Docker Test Environment

`docker-compose.yml` provides InfluxDB v2.7 with:
- Organization: `test-org`
- Bucket: `netscan`
- Token: `netscan-token`

## Usage

### Full Discovery Mode (Default)

```bash
./netscan
```

Performs ICMP sweep across configured networks, then SNMP polling of online devices.

### ICMP-Only Mode

```bash
./netscan --icmp-only
```

ICMP discovery only, configurable via `icmp_discovery_interval`.

### Custom Config

```bash
./netscan -config /path/to/config.yml
```

## Deployment

### Automated (Recommended)

```bash
sudo ./deploy.sh
```

Creates:
- `/opt/netscan/` with binary and config
- `netscan` user with minimal privileges
- `CAP_NET_RAW` capability on binary
- Systemd service with network-compatible security settings

### Manual

```bash
go build -o netscan ./cmd/netscan
sudo mkdir -p /opt/netscan
sudo cp netscan /opt/netscan/
sudo cp config.yml /opt/netscan/
sudo setcap cap_net_raw+ep /opt/netscan/netscan
sudo useradd -r -s /bin/false netscan
sudo chown -R netscan:netscan /opt/netscan

sudo tee /etc/systemd/system/netscan.service > /dev/null <<EOF
[Unit]
Description=netscan network monitoring
After=network.target

[Service]
Type=simple
ExecStart=/opt/netscan/netscan
WorkingDirectory=/opt/netscan
Restart=always
User=netscan
Group=netscan

# Security settings (relaxed for network access)
NoNewPrivileges=yes
PrivateTmp=yes
ProtectSystem=strict
AmbientCapabilities=CAP_NET_RAW

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable netscan
sudo systemctl start netscan
```

## Service Management

```bash
sudo systemctl status netscan
sudo journalctl -u netscan -f
sudo systemctl restart netscan
sudo systemctl stop netscan
```

## Testing

```bash
go test ./...                    # All tests
go test -v ./...                 # Verbose output
go test ./internal/config        # Specific package
go test -race ./...              # Race detection
go test -cover ./...             # Coverage report
```

## Troubleshooting

### ICMP Permission Denied
```bash
# Manual execution
sudo ./netscan --icmp-only

# Check capability
getcap /usr/local/bin/netscan
```

### InfluxDB Connection Issues
- Verify InfluxDB running: `sudo docker ps`
- Check config credentials
- Validate API token permissions

### No Devices Discovered
- Verify network ranges in config
- Check firewall rules for ICMP/SNMP
- Use `--icmp-only` for broader discovery

### Performance Issues
- **Monitor Real Usage**: Use `htop` or `top` to observe actual CPU/memory consumption
- **Start Conservative**: Begin with lower worker counts (8 ICMP, 4 SNMP) and increase gradually
- **SNMP Bottleneck**: SNMP operations are more CPU-intensive than ICMP pings
- **Network Latency**: High latency networks may require fewer concurrent operations
- **Memory Growth**: Monitor for memory leaks with long-running processes

## Performance Tuning

- **ICMP Workers**: 64 concurrent ping operations (lightweight, network-bound)
- **SNMP Workers**: 32 concurrent SNMP queries (CPU-intensive protocol parsing)
- **Memory**: ~50MB baseline + ~1KB per monitored device
- **Scaling**: Adjust worker counts based on CPU cores and network size

#### Recommended Worker Counts by System Size

| System Type | CPU Cores | ICMP Workers | SNMP Workers | Max Devices |
|-------------|-----------|--------------|--------------|-------------|
| Raspberry Pi | 4 | 4-8 | 2-4 | 50-100 |
| Home Server | 4-8 | 8-16 | 4-8 | 200-500 |
| Workstation | 8-16 | 16-32 | 8-16 | 500-1000 |
| Server | 16+ | 32-64 | 16-32 | 1000+ |

#### Default Worker Counts

The default configuration (64 ICMP, 32 SNMP workers) is optimized for:
- **High-performance servers** (16+ CPU cores)
- **Large enterprise networks** (/16+ CIDR ranges)
- **Low-latency networks** (<1ms average ping times)

**For most systems**, start with more conservative values:
```yaml
icmp_workers: 8   # 2-4x CPU cores
snmp_workers: 4   # 1-2x CPU cores
```

Monitor actual CPU usage and adjust based on your specific environment.

#### Performance Characteristics (Estimated)

**Note**: Performance numbers are estimates based on typical Go application behavior and network monitoring patterns. Actual performance varies by:
- Network latency and reliability
- Device response times
- System I/O capabilities
- Go runtime scheduling overhead

| System Type | CPU Cores | ICMP Workers | SNMP Workers | Est. CPU % | Concurrent Ops |
|-------------|-----------|--------------|--------------|------------|----------------|
| Raspberry Pi 4 | 4 | 4-8 | 2-4 | 10-25% | 6-12 |
| Home Server | 4-8 | 8-16 | 4-8 | 15-35% | 12-24 |
| Workstation | 8-16 | 16-32 | 8-16 | 20-45% | 24-48 |
| Enterprise Server | 16+ | 32-64 | 16-32 | 30-60% | 48-96 |

#### Real-World Testing Recommendations

```bash
# Monitor actual CPU usage
watch -n 1 "ps aux | grep netscan | grep -v grep"

# Test different worker counts
# Start with conservative values and increase gradually
icmp_workers: 8   # Start low, monitor CPU
snmp_workers: 4   # SNMP is more CPU intensive

# Use system monitoring tools
htop    # Real-time CPU/memory monitoring
iotop   # I/O monitoring
nload   # Network bandwidth monitoring
```

#### Performance Factors

- **ICMP Operations**: ~0.1-0.5ms CPU time per ping (mostly network wait)
- **SNMP Operations**: ~5-50ms CPU time per query (protocol processing)
- **Go Goroutines**: ~2-8KB memory per goroutine
- **Channel Operations**: Minimal overhead with buffered channels

## Implementation Details

### Discovery Process
1. ICMP sweep: Configurable concurrent workers ping all IPs in CIDR ranges
2. SNMP polling: Configurable concurrent workers query online devices for MIB-II data
3. State management: Devices tracked with last-seen timestamps
4. Pruning: Devices removed after 2 * discovery_interval without sightings

### Concurrency Model
- Producer-consumer pattern with buffered channels (256 slots)
- Context-based cancellation for graceful shutdown
- sync.WaitGroup for worker lifecycle management

### Metrics Storage
- Measurement: "ping"
- Tags: "ip", "hostname"
- Fields: "rtt_ms" (float), "success" (boolean), "snmp_name" (string), "snmp_description" (string), "snmp_sysid" (string)
- Point-based writes with error handling

### Security Model
- Linux capabilities: CAP_NET_RAW for raw socket access
- Dedicated service user: Non-root execution
- Systemd hardening: NoNewPrivileges, PrivateTmp, ProtectSystem with AmbientCapabilities for network access

## Development

### Development Setup

#### Prerequisites
- Go 1.21+ (tested with 1.25.1)
- Git
- InfluxDB 2.x (for testing)

#### Clone Repository
```bash
git clone https://github.com/extkljajicm/netscan.git
cd netscan
go mod download
```

#### Test Environment Setup
```bash
# Start InfluxDB test instance
sudo docker-compose up -d

# Verify setup
go build -o netscan ./cmd/netscan
go test ./...
```

### Development Workflow

#### Feature Development
1. Create feature branch from main
   ```bash
   git checkout -b feature/new-monitoring-feature
   ```

2. Implement changes with conventional commits
   ```bash
   git add .
   git commit -m "feat: add advanced ICMP timeout handling"
   ```

3. Push branch and create pull request
   ```bash
   git push origin feature/new-monitoring-feature
   # Create pull request on GitHub
   ```

#### Conventional Commit Format
```
feat: add new monitoring feature
fix: resolve memory leak in worker pools
perf: optimize SNMP discovery concurrency
docs: update configuration documentation
test: add unit tests for config parsing
refactor: restructure state management
chore: update dependencies
```

#### Integration Testing
- CI/CD pipeline triggers on push/PR
- Runs tests with race detection
- Builds binaries for validation
- Reports code coverage
- Validates configuration and deployment scripts

#### Release Process
1. Merge approved changes to main
   ```bash
   git checkout main
   git pull origin main
   ```

2. Create version tag when ready for release
   ```bash
   git tag v1.0.0
   git push origin main --tags
   ```

3. Automated release process:
   - Changelog generation from conventional commits
   - CHANGELOG.md update with new version section
   - Multi-platform binary builds (Linux/macOS/Windows, AMD64/ARM64)
   - Release archives creation (.tar.gz packages)
   - GitHub release creation with attached binaries

#### Version Strategy
- Alpha/Beta: v0.x.x-alpha.x, v0.x.x-beta.x
- Stable: v1.x.x, v2.x.x
- Patch: v1.0.1, v1.0.2

### Code Quality

```bash
go fmt ./...    # Format code
go vet ./...    # Static analysis
go mod tidy     # Clean dependencies
```

### Project Structure
```
netscan/
├── cmd/netscan/           # CLI application
├── internal/              # Private packages
│   ├── config/           # Configuration parsing
│   ├── discovery/        # Network scanning
│   ├── monitoring/       # Ping monitoring
│   ├── state/            # Device state
│   └── influx/           # Metrics storage
├── docker-compose.yml    # Test environment
├── build.sh             # Build automation
├── deploy.sh            # Deployment script
└── config.yml.example   # Configuration template
```

## License

MIT
