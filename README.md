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

## Quick Start with Docker (Recommended)

The easiest way to get netscan running is with Docker Compose, which sets up both netscan and InfluxDB automatically.

### Prerequisites

- Docker Engine 20.10+
- Docker Compose V2

### Installation Steps

1. **Clone the repository**
   ```bash
   git clone https://github.com/extkljajicm/netscan.git
   cd netscan
   ```

2. **Create and configure config.yml**
   ```bash
   # Copy the example configuration
   cp config.yml.example config.yml
   
   # Edit the configuration with your network settings
   nano config.yml
   ```
   
   **Important**: Update these settings in config.yml:
   - `networks`: Add your network ranges (e.g., `192.168.1.0/24`)
   - `influxdb.token`: Set to `netscan-token` (matches docker-compose default)
   - `influxdb.org`: Set to `test-org` (matches docker-compose default)
   - `snmp.community`: Set to `public` (default SNMP community string)
   
   For testing, you can use sed to quickly replace the placeholders:
   ```bash
   sed -i 's|\${INFLUXDB_TOKEN}|netscan-token|g' config.yml
   sed -i 's|\${INFLUXDB_ORG}|test-org|g' config.yml
   sed -i 's|\${SNMP_COMMUNITY}|public|g' config.yml
   ```

3. **Start the stack**
   ```bash
   docker compose up -d
   ```
   
   This will:
   - Build the netscan Docker image from the local Dockerfile
   - Start InfluxDB with persistent storage
   - Start netscan with network monitoring capabilities

4. **Verify it's running**
   ```bash
   # Check container status
   docker compose ps
   
   # View netscan logs
   docker compose logs -f netscan
   
   # View InfluxDB logs
   docker compose logs -f influxdb
   ```

5. **Access InfluxDB UI** (optional)
   
   Open http://localhost:8086 in your browser
   - Username: `admin`
   - Password: `admin123`
   - Organization: `test-org`
   - Bucket: `netscan`

### Managing the Docker Stack

```bash
# Stop services
docker compose down

# Stop and remove all data (including InfluxDB storage)
docker compose down -v

# Restart services
docker compose restart

# Rebuild after code changes
docker compose up -d --build

# View real-time logs
docker compose logs -f

# View logs for specific service
docker compose logs -f netscan
```

### Docker Configuration Details

The `docker-compose.yml` configures:

- **netscan service**:
  - Builds from local Dockerfile (Go 1.25)
  - Uses `host` network mode for ICMP/SNMP access to your network
  - Has `CAP_NET_RAW` capability for raw socket access (ICMP ping)
  - Mounts `config.yml` as read-only
  - Auto-restarts on failure

- **influxdb service**:
  - InfluxDB 2.7 for metrics storage
  - Exposed on port 8086
  - Default credentials (change for production):
    - Token: `netscan-token`
    - Org: `test-org`
    - Bucket: `netscan`
  - Persistent volume for data retention

### Customizing for Production

For production use, update the InfluxDB credentials in `docker-compose.yml`:

```yaml
environment:
  - DOCKER_INFLUXDB_INIT_USERNAME=your-admin-user
  - DOCKER_INFLUXDB_INIT_PASSWORD=your-secure-password
  - DOCKER_INFLUXDB_INIT_ORG=your-org
  - DOCKER_INFLUXDB_INIT_ADMIN_TOKEN=your-secure-token
```

Then update your `config.yml` to match:
```yaml
influxdb:
  url: "http://localhost:8086"
  token: "your-secure-token"
  org: "your-org"
  bucket: "netscan"
```

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

### Setup for Native Build
```bash
git clone https://github.com/extkljajicm/netscan.git
cd netscan
go mod download
```

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

## Deployment Options

### Option 1: Docker Compose (Recommended)

See the [Quick Start with Docker](#quick-start-with-docker-recommended) section above for complete instructions.

### Option 2: Native Installation with Systemd

#### Automated Deployment

```bash
sudo ./deploy.sh
```

Creates:
- `/opt/netscan/` with binary, config, and secure `.env` file
- `netscan` user with minimal privileges
- `CAP_NET_RAW` capability on binary
- Systemd service with network-compatible security settings
- Secure credential management via environment variables

#### Manual Deployment

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

### Docker Compose

```bash
# View all services status
docker compose ps

# View logs (all services)
docker compose logs -f

# View logs (specific service)
docker compose logs -f netscan
docker compose logs -f influxdb

# Restart services
docker compose restart

# Stop services (keeps data)
docker compose down

# Stop and remove all data
docker compose down -v

# Rebuild and restart after changes
docker compose up -d --build
```

### Systemd (Native Installation)

```bash
# Check service status
sudo systemctl status netscan

# View logs
sudo journalctl -u netscan -f

# Restart service
sudo systemctl restart netscan

# Stop service
sudo systemctl stop netscan

# Start service
sudo systemctl start netscan
```

## Building

### Docker Image

The Docker image is built locally using the Dockerfile when you run `docker compose up`.

```bash
# Build the image manually
docker compose build

# Or build with docker directly
docker build -t netscan:local .
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

**Docker Images:**
- Linux (amd64) - Built locally via Dockerfile

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
# Start test environment with Docker Compose
docker compose up -d

# View logs to verify it's working
docker compose logs -f netscan
```

### CI/CD Pipeline

Automated testing runs on push, pull requests, and version tags.

**Features:**
- Docker Compose stack validation
- Linux amd64 binary builds
- Automated changelog generation
- Code coverage reporting
- Release artifact creation

## Troubleshooting

### Docker Issues

**Container keeps restarting:**
```bash
# Check logs for errors
docker compose logs netscan

# Common causes:
# 1. Invalid config.yml - verify syntax and network ranges
# 2. InfluxDB credentials mismatch - check token and org match docker-compose.yml
# 3. Missing config.yml - ensure file exists and is mounted correctly
```

**InfluxDB connection failed:**
```bash
# Verify InfluxDB is healthy
docker compose ps influxdb

# Check if InfluxDB is accessible
curl http://localhost:8086/health

# Verify credentials in config.yml match docker-compose.yml:
# - token: netscan-token
# - org: test-org
```

**Can't access network devices:**
```bash
# Verify host network mode is enabled
docker inspect netscan | grep NetworkMode

# Check CAP_NET_RAW capability
docker inspect netscan | grep -A 5 CapAdd
```

### Native Installation Issues

**ICMP Permission Denied:**
```bash
sudo ./netscan          # Manual execution
getcap /opt/netscan/netscan  # Check capability
```

**InfluxDB Connection Issues:**
- Verify InfluxDB running: `systemctl status influxdb` or `docker ps`
- Check config credentials and API token
- Confirm network connectivity

### General Issues

**No Devices Discovered:**
- Verify network ranges in config.yml (e.g., `192.168.1.0/24`)
- Check firewall rules for ICMP/SNMP
- Confirm SNMP community string is correct
- Test with ping manually: `ping <device-ip>`

**Performance Issues:**
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
