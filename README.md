# netscan

Network monitoring service written in Go that performs ICMP ping monitoring of discovered devices and stores metrics in InfluxDB.

> **📝 Note:** This README focuses on Docker deployment. For native installation (systemd, manual builds), see [README_NATIVE.md](README_NATIVE.md).

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
- **Docker Deployment**: Complete stack with Docker Compose
- **Security**: Linux capabilities (CAP_NET_RAW) for non-root ICMP access, input validation, and secure credential handling

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

## Quick Start with Docker

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
   
   **Note**: The InfluxDB credentials and SNMP community string use environment variables that are automatically provided by docker-compose.yml:
   - `${INFLUXDB_TOKEN}` → `netscan-token` (default)
   - `${INFLUXDB_ORG}` → `test-org` (default)
   - `${SNMP_COMMUNITY}` → `public` (default)
   
   You can leave these as-is in config.yml. To customize for production, edit the `environment` section in `docker-compose.yml`.

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
  - Environment variables for credential expansion:
    - `INFLUXDB_TOKEN=netscan-token`
    - `INFLUXDB_ORG=test-org`
    - `SNMP_COMMUNITY=public`
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

For production use, update both the InfluxDB initialization and netscan environment variables in `docker-compose.yml`:

```yaml
services:
  netscan:
    environment:
      - INFLUXDB_TOKEN=your-secure-token
      - INFLUXDB_ORG=your-org
      - SNMP_COMMUNITY=your-community-string
  
  influxdb:
    environment:
      - DOCKER_INFLUXDB_INIT_USERNAME=your-admin-user
      - DOCKER_INFLUXDB_INIT_PASSWORD=your-secure-password
      - DOCKER_INFLUXDB_INIT_ORG=your-org
      - DOCKER_INFLUXDB_INIT_ADMIN_TOKEN=your-secure-token
```

The config.yml file can remain unchanged - environment variables will be automatically expanded:
```yaml
influxdb:
  url: "http://localhost:8086"
  token: "${INFLUXDB_TOKEN}"  # Automatically expanded from docker-compose environment
  org: "${INFLUXDB_ORG}"      # Automatically expanded from docker-compose environment
  bucket: "netscan"

snmp:
  community: "${SNMP_COMMUNITY}"  # Automatically expanded from docker-compose environment
```

## Configuration

### For Docker Deployment

The configuration is handled through `config.yml`. Copy and edit the template:

```bash
cp config.yml.example config.yml
```

Edit the following key settings:
- `networks`: Your network ranges (e.g., `192.168.1.0/24`)
- `influxdb.token`: `netscan-token` (matches docker-compose default)
- `influxdb.org`: `test-org` (matches docker-compose default)
- `snmp.community`: `public` (default SNMP community string)

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

### Docker Environment

The `docker-compose.yml` provides a complete stack with:
- InfluxDB v2.7 for metrics storage
- Organization: `test-org`
- Bucket: `netscan`
- Token: `netscan-token`

## Service Management

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

## Building

The Docker image is built automatically when you run `docker compose up`. To build manually:

```bash
# Build the image with docker compose
docker compose build

# Or build with docker directly
docker build -t netscan:local .
```

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
- Linux capabilities: CAP_NET_RAW for raw socket access (provided by Docker)
- Dedicated service user: Non-root execution in container
- Docker security: Minimal Alpine base image, non-root user, minimal capabilities

## Development

### Local Development with Docker

```bash
git clone https://github.com/extkljajicm/netscan.git
cd netscan

# Start InfluxDB for testing
docker compose up -d influxdb

# Run tests
go test ./...
go test -race ./...  # Race detection

# Build and run with changes
docker compose up -d --build
```

### Code Quality

```bash
go fmt ./...    # Format code
go vet ./...    # Static analysis
go mod tidy     # Clean dependencies
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
