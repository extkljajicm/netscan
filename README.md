# netscan

Network monitoring service written in Go that performs ICMP ping monitoring of discovered devices and stores metrics in InfluxDB.

## Overview

Performs decoupled network discovery and monitoring with four independent tickers: ICMP discovery finds new devices, scheduled SNMP scans enrich device data, pinger reconciliation ensures continuous monitoring, and state pruning removes stale devices. This architecture provides efficient resource usage and fast response to network changes.

## Features

- **Multi-Ticker Architecture**: Four independent tickers for decoupled operations
  - ICMP Discovery (every 5m) - Finds new responsive devices
  - Daily SNMP Scan (scheduled) - Enriches all devices with SNMP data
  - Pinger Reconciliation (every 5s) - Ensures all devices monitored
  - State Pruning (every 1h) - Removes stale devices
- **Dual-Trigger SNMP**: Immediate scan for new devices + scheduled daily scan for all devices
- **Concurrent Processing**: Configurable worker pool patterns for scalable network operations
- **State-Centric Design**: StateManager as single source of truth for all devices
- **InfluxDB v2**: Time-series metrics storage with point-based writes
- **Configuration**: YAML-based config with duration parsing and environment variable support
- **Security**: Linux capabilities (CAP_NET_RAW) for non-root ICMP access, input validation, and secure credential handling
- **Single Binary**: No runtime dependencies

## Architecture

The application uses a **multi-ticker architecture** with four independent, concurrent loops:

1. **ICMP Discovery Ticker** - Scans networks for responsive devices
2. **Daily SNMP Scan Ticker** - Enriches devices with SNMP data at scheduled time
3. **Pinger Reconciliation Ticker** - Ensures all devices have active monitoring
4. **State Pruning Ticker** - Removes devices not seen in 24 hours

```
cmd/netscan/main.go           # Four-ticker orchestration and CLI interface
internal/
├── config/config.go          # YAML parsing with SNMPDailySchedule support
├── discovery/scanner.go      # RunICMPSweep() and RunSNMPScan() functions
├── monitoring/pinger.go      # ICMP monitoring with StateManager integration
├── state/manager.go          # Thread-safe device state (single source of truth)
└── influx/writer.go          # InfluxDB client wrapper with health checks
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

### Security Features

- **Environment Variables**: Sensitive values (tokens, passwords) can use environment variables with `${VAR_NAME}` syntax
- **Secure .env File**: Deployment creates a separate `.env` file with restrictive permissions (600) for sensitive credentials
- **Input Validation**: Configuration is validated at startup for security and sanity
- **Network Range Validation**: Prevents scanning dangerous networks (loopback, multicast, link-local, overly broad ranges)
- **Runtime Validation**: SNMP responses, IP addresses, and database writes are validated and sanitized
- **SNMP Security**: Community string validation with weak password detection
- **Resource Protection**: Configurable limits prevent DoS attacks and resource exhaustion
  - Rate limiting for discovery scans and database writes
  - Memory usage monitoring with configurable limits
  - Concurrent operation bounds to prevent goroutine exhaustion
  - Device count limits with automatic cleanup

### Environment Variables

Sensitive configuration values are loaded from a `.env` file created during deployment:

```bash
# .env file (created by deploy.sh with 600 permissions)
INFLUXDB_URL=http://localhost:8086      # InfluxDB server URL
INFLUXDB_TOKEN=netscan-token            # InfluxDB API token
INFLUXDB_ORG=test-org                   # InfluxDB organization
INFLUXDB_BUCKET=netscan                 # InfluxDB bucket name
SNMP_COMMUNITY=your-community           # SNMPv2c community string
```

**Supported Environment Variables:**
- `INFLUXDB_URL`: InfluxDB server endpoint
- `INFLUXDB_TOKEN`: API token for authentication
- `INFLUXDB_ORG`: Organization name
- `INFLUXDB_BUCKET`: Target bucket for metrics
- `SNMP_COMMUNITY`: SNMPv2c community string for device access

**Security Best Practices:**
- Never commit `.env` files to version control
- Set restrictive permissions: `chmod 600 .env`
- Rotate credentials regularly
- Use strong, unique tokens for each environment

**For Production:**
- Generate unique, strong tokens for InfluxDB
- Use different organizations per environment
- Change SNMP community strings from defaults
- Consider using a secret management system

### Configuration Structure

```yaml
# Network Discovery
networks:
  - "192.168.0.0/24"
  - "10.0.0.0/16"

icmp_discovery_interval: "5m"      # How often to scan for new devices
snmp_daily_schedule: "02:00"       # Daily SNMP scan time (HH:MM)

# SNMP Settings
snmp:
  community: "${SNMP_COMMUNITY}"   # From .env file
  port: 161
  timeout: "5s"
  retries: 1

# Monitoring
ping_interval: "2s"
ping_timeout: "2s"

# Performance
icmp_workers: 64                   # Concurrent ICMP workers
snmp_workers: 32                   # Concurrent SNMP workers

# InfluxDB (credentials from .env file)
influxdb:
  url: "${INFLUXDB_URL}"
  token: "${INFLUXDB_TOKEN}"
  org: "${INFLUXDB_ORG}"
  bucket: "netscan"

# Resource Limits
max_concurrent_pingers: 1000
max_devices: 10000
min_scan_interval: "1m"
memory_limit_mb: 512
```

### Docker Test Environment

`docker-compose.yml` provides InfluxDB v2.7 with:
- Organization: `test-org`
- Bucket: `netscan`
- Token: `netscan-token`

## Usage

### Standard Mode

```bash
./netscan
```

Runs the multi-ticker architecture with:
- ICMP discovery every 5 minutes (configurable via `icmp_discovery_interval`)
- Daily SNMP scan at configured time (e.g., 02:00)
- Continuous monitoring of all discovered devices
- Automatic state pruning of stale devices

### Custom Config

```bash
./netscan -config /path/to/config.yml
```

### Command Line Options

- `-config string`: Path to configuration file (default "config.yml")
- `-help`: Display usage information

## Deployment

### Docker (Recommended for Containers)

The netscan application is available as a Docker image from GitHub Container Registry.

#### Quick Start with Docker Compose

```bash
# Create config.yml from template
cp config.yml.example config.yml

# Edit config.yml with your settings
nano config.yml

# Start netscan with InfluxDB using docker-compose
docker-compose -f docker-compose.netscan.yml up -d

# View logs
docker-compose -f docker-compose.netscan.yml logs -f netscan

# Stop services
docker-compose -f docker-compose.netscan.yml down
```

#### Pull and Run Docker Image

```bash
# Pull the latest image
docker pull ghcr.io/extkljajicm/netscan:latest

# Run with host networking (required for ICMP/SNMP access)
docker run -d \
  --name netscan \
  --network host \
  --cap-add=NET_RAW \
  -v $(pwd)/config.yml:/app/config.yml:ro \
  -e INFLUXDB_TOKEN=your-token \
  -e SNMP_COMMUNITY=your-community \
  ghcr.io/extkljajicm/netscan:latest
```

#### Build Docker Image Locally

```bash
# Build the image
docker build -t netscan:local .

# Run the locally built image
docker run -d \
  --name netscan \
  --network host \
  --cap-add=NET_RAW \
  -v $(pwd)/config.yml:/app/config.yml:ro \
  netscan:local
```

**Docker Configuration Notes:**
- **Network Mode**: Use `--network host` to allow netscan to access network devices for ICMP and SNMP
- **Capabilities**: `--cap-add=NET_RAW` is required for ICMP ping functionality
- **Config File**: Mount your `config.yml` as a read-only volume
- **Environment Variables**: Pass sensitive credentials via environment variables instead of storing in config
- **Available Tags**: `latest`, `main`, version tags (e.g., `v1.0.0`), and commit SHAs

### Native Installation (Automated)

```bash
sudo ./deploy.sh
```

Creates:
- `/opt/netscan/` with binary, config, and secure `.env` file
- `netscan` user with minimal privileges
- `CAP_NET_RAW` capability on binary
- Systemd service with network-compatible security settings
- Secure credential management via environment variables

### Native Installation (Manual)

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

### Systemd (Native Installation)

```bash
sudo systemctl status netscan
sudo journalctl -u netscan -f
sudo systemctl restart netscan
sudo systemctl stop netscan
```

### Docker

```bash
# View container status
docker ps -f name=netscan

# View logs
docker logs -f netscan

# Restart container
docker restart netscan

# Stop container
docker stop netscan

# Remove container
docker rm netscan
```

## Building

### Docker Image

Docker images are automatically built and published via GitHub Actions on every push to main and on version tags. See `.github/workflows/dockerize_netscan.yml` for details.

```bash
# Build locally
docker build -t netscan:local .

# Build for multiple platforms (requires buildx)
docker buildx build --platform linux/amd64,linux/arm64 -t netscan:local .
```

### Native Binary

#### Automated Build

```bash
./build.sh
```

Builds the netscan binary with optimized settings.

#### Manual Build

```bash
go build -o netscan ./cmd/netscan
```

#### Cross-Platform Builds

**Docker Images (via GitHub Actions):**
- Linux (amd64)
- Linux (arm64)

**Native Binaries (via CI/CD):**
- Linux (amd64)

For custom platform builds, use Go's cross-compilation:
```bash
GOOS=linux GOARCH=arm64 go build -o netscan-arm64 ./cmd/netscan
GOOS=darwin GOARCH=amd64 go build -o netscan-macos ./cmd/netscan
```

## Testing

### Unit Tests

```bash
go test ./...                    # All tests
go test -v ./...                 # Verbose output
go test ./internal/config        # Specific package
go test -race ./...              # Race detection
go test -cover ./...             # Coverage report
```

**Test Coverage:**
- Configuration parsing and validation
- Network discovery algorithms
- State management concurrency
- InfluxDB client operations
- ICMP ping monitoring
- SNMP polling functionality

### Integration Testing

```bash
# Start test InfluxDB
sudo docker-compose up -d

# Run netscan with test config
./netscan -config config.yml
```

### CI/CD Pipeline

Automated testing runs on push, pull requests, and version tags.

**Features:**
- Linux amd64 binary builds
- Automated changelog generation
- Code coverage reporting
- Release artifact creation

## Troubleshooting

### ICMP Permission Denied
```bash
sudo ./netscan          # Manual execution
getcap /opt/netscan/netscan  # Check capability
```

### InfluxDB Connection Issues
- Verify InfluxDB running: `docker ps`
- Check config credentials and API token
- Confirm network connectivity

### No Devices Discovered
- Verify network ranges in config
- Check firewall rules for ICMP/SNMP
- Confirm SNMP community string is correct

### Performance Issues
- Start with lower worker counts (8 ICMP, 4 SNMP)
- Monitor CPU usage with `htop` or `top`
- Adjust based on network latency and CPU cores

## Performance Tuning

**Default Configuration:**
- ICMP Workers: 64 (lightweight, network-bound operations)
- SNMP Workers: 32 (CPU-intensive protocol parsing)
- Memory: ~50MB baseline + ~1KB per device

**Recommended Worker Counts:**

| System Type | CPU Cores | ICMP Workers | SNMP Workers | Max Devices |
|-------------|-----------|--------------|--------------|-------------|
| Raspberry Pi | 4 | 8 | 4 | 100 |
| Home Server | 4-8 | 16 | 8 | 500 |
| Workstation | 8-16 | 32 | 16 | 1000 |
| Server | 16+ | 64 | 32 | 10000 |

Start with conservative values and monitor CPU usage to adjust for your environment.

## Implementation Details

### Discovery Process (Multi-Ticker Architecture)

The new architecture uses four independent tickers:

1. **ICMP Discovery Ticker** (every `icmp_discovery_interval`, default 5m):
   - Performs ICMP ping sweep across all configured networks
   - Adds responsive IPs to StateManager
   - Triggers immediate SNMP scan for newly discovered devices

2. **Daily SNMP Scan Ticker** (at `snmp_daily_schedule`, e.g., 02:00):
   - Full SNMP scan of all devices in StateManager
   - Enriches devices with hostname and sysDescr
   - Updates device info in InfluxDB

3. **Pinger Reconciliation Ticker** (every 5 seconds):
   - Ensures every device in StateManager has an active pinger goroutine
   - Starts pingers for new devices
   - Stops pingers for removed devices
   - Maintains consistency between state and running pingers

4. **State Pruning Ticker** (every 1 hour):
   - Removes devices not seen in last 24 hours
   - Prevents memory growth from stale devices

### Concurrency Model
- Producer-consumer pattern with buffered channels (256 slots)
- Context-based cancellation for graceful shutdown
- sync.WaitGroup for worker lifecycle management

### Metrics Storage
- Measurement: "ping"
- Tags: "ip", "hostname"
- Fields: "rtt_ms" (float), "success" (boolean)
- Point-based writes with error handling

- Measurement: "device_info"
- Tags: "ip"
- Fields: "hostname" (string), "snmp_description" (string)
- Stored once per device or when SNMP data changes

### Security Model
- Linux capabilities: CAP_NET_RAW for raw socket access
- Dedicated service user: Non-root execution
- Systemd hardening: NoNewPrivileges, PrivateTmp, ProtectSystem with AmbientCapabilities for network access

## Development

### Setup

```bash
git clone https://github.com/extkljajicm/netscan.git
cd netscan
go mod download
docker-compose up -d  # Start test InfluxDB
go test ./...         # Run tests
```

### Code Quality

```bash
go fmt ./...    # Format code
go vet ./...    # Static analysis
go mod tidy     # Clean dependencies
go test -race ./...  # Race detection
```

### Conventional Commits

```
feat: add new feature
fix: resolve bug
perf: optimize performance
docs: update documentation
test: add tests
refactor: restructure code
```

### Project Structure
```
netscan/
├── cmd/netscan/           # CLI application and main orchestration
│   ├── main.go           # Service startup, signal handling, discovery loops
│   └── main_test.go      # Basic package test placeholder
├── internal/              # Private application packages
│   ├── config/           # Configuration parsing and validation
│   │   ├── config.go     # YAML loading, environment expansion, validation
│   │   └── config_test.go # Configuration parsing tests
│   ├── discovery/        # Network device discovery
│   │   ├── scanner.go    # ICMP/SNMP concurrent scanning workers
│   │   └── scanner_test.go # Discovery algorithm tests
│   ├── monitoring/       # Continuous device monitoring
│   │   ├── pinger.go     # ICMP ping goroutines with metrics collection
│   │   └── pinger_test.go # Ping monitoring tests
│   ├── state/            # Thread-safe device state management
│   │   ├── manager.go    # RWMutex-protected device registry
│   │   └── manager_test.go # State management concurrency tests
│   └── influx/           # Time-series data persistence
│       ├── writer.go     # InfluxDB client wrapper with rate limiting
│       └── writer_test.go # Database write operation tests
├── docker-compose.yml    # Test environment (InfluxDB v2.7)
├── build.sh             # Simple binary build script
├── deploy.sh            # Production deployment automation
├── undeploy.sh          # Complete uninstallation script
├── config.yml.example   # Configuration template
├── cliff.toml           # Changelog generation configuration
└── .github/workflows/   # CI/CD automation
    └── ci-cd.yml        # GitHub Actions pipeline
```

## License

MIT
