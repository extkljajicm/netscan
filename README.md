# netscan

Production-grade network monitoring service written in Go (1.25+) for linux-amd64. Performs automated ICMP discovery, continuous ping monitoring, and SNMP metadata collection with time-series metrics storage in InfluxDB v2.

---

## Section 1: Docker Deployment (Recommended)

Docker deployment provides the easiest path to get netscan running with automatic orchestration of the complete stack (netscan + InfluxDB).

### Prerequisites

* Docker Engine 20.10+
* Docker Compose V2
* Network access to target devices

### Installation Steps

**1. Clone Repository**
```bash
git clone https://github.com/kljama/netscan.git
cd netscan
```

**2. Create Configuration File**
```bash
cp config.yml.example config.yml
```

Edit `config.yml` and update the `networks` section with your actual network ranges:
```yaml
networks:
  - "192.168.1.0/24"    # YOUR actual network range
  - "10.0.50.0/24"      # Add additional ranges as needed
```

**CRITICAL:** The example networks (192.168.0.0/24, etc.) are placeholders. If these don't match your network, netscan will find 0 devices. Use `ip addr` (Linux) or `ipconfig` (Windows) to determine your network range.

**3. (Optional) Configure Credentials**

For production security, create a `.env` file instead of using default credentials:
```bash
cp .env.example .env
chmod 600 .env
```

Edit `.env` and set secure values:
```bash
INFLUXDB_TOKEN=<generate-with-openssl-rand-base64-32>
DOCKER_INFLUXDB_INIT_ADMIN_TOKEN=<same-as-INFLUXDB_TOKEN>
DOCKER_INFLUXDB_INIT_PASSWORD=<strong-password>
SNMP_COMMUNITY=<your-snmp-community-not-public>
```

The `.env` file is automatically loaded by Docker Compose. Variables are expanded in `config.yml` (e.g., `${INFLUXDB_TOKEN}`).

**4. Start Stack**
```bash
docker compose up -d
```

This builds the netscan image from the local Dockerfile and starts both netscan and InfluxDB v2.7.

**5. Verify Operation**
```bash
# Check container status
docker compose ps

# View netscan logs
docker compose logs -f netscan

# Check health endpoint
curl http://localhost:8080/health | jq
```

**6. Access InfluxDB UI (Optional)**

Navigate to http://localhost:8086 in your browser:
* Username: `admin`
* Password: `admin123` (or your `.env` value)
* Organization: `test-org`
* Bucket: `netscan`

### Service Management

```bash
# Stop services (keeps data)
docker compose down

# Stop and remove all data
docker compose down -v

# Restart services
docker compose restart

# Rebuild after code changes
docker compose up -d --build

# View logs
docker compose logs -f netscan
docker compose logs -f influxdb
```

### Configuration Details

The `docker-compose.yml` configures:

* **netscan service:**
  * Builds from local Dockerfile (multi-stage, Go 1.25, ~15MB final image)
  * `network_mode: host` - Direct access to host network for ICMP/SNMP
  * `cap_add: NET_RAW` - Linux capability for raw ICMP sockets
  * Runs as root (required for ICMP in containers, see Security Notes)
  * Mounts `config.yml` as read-only volume at `/app/config.yml`
  * Environment variables from `.env` file or defaults
  * Log rotation configured (10MB max per file, 3 files retained, ~30MB total)
  * HEALTHCHECK on `/health/live` endpoint (30s interval, 3s timeout, 3 retries, 40s start period)
  * Auto-restart on failure

* **influxdb service:**
  * InfluxDB v2.7 official image
  * Exposed on port 8086
  * Environment variables from `.env` file or defaults
  * Persistent volume `influxdbv2-data` for data retention
  * Health check using `influx ping`

### Security Notes

**Docker Deployment:**
* Container runs as root user (non-negotiable requirement for ICMP raw socket access in Linux containers)
* CAP_NET_RAW capability provides raw socket access without full privileged mode
* Container remains isolated from host through Docker namespace isolation
* Minimal Alpine Linux base image (~15MB) reduces attack surface
* Config file mounted read-only (`:ro`)

**Security Trade-off:** Root access is required for ping functionality, but Docker containerization provides security boundary. For maximum security without root, use Native Deployment (Section 2).

### Troubleshooting

**Container finds 0 devices:**
```bash
# 1. Verify config.yml exists and is mounted
docker exec -it netscan cat /app/config.yml | grep -A 5 "networks:"

# 2. Check networks being scanned
docker compose logs netscan | grep "Scanning networks"

# 3. Verify host network mode
docker inspect netscan | grep NetworkMode
# Should show: "NetworkMode": "host"

# 4. Test ping manually
docker exec -it netscan ping -c 2 192.168.1.1
# Replace 192.168.1.1 with an IP from your network
```

**Container keeps restarting:**
```bash
# Check logs for errors
docker compose logs netscan

# Common causes:
# - Invalid config.yml syntax
# - InfluxDB credentials mismatch
# - Missing config.yml file
```

**InfluxDB connection failed:**
```bash
# Verify InfluxDB is healthy
docker compose ps influxdb

# Check InfluxDB health endpoint
curl http://localhost:8086/health

# Verify credentials in config.yml match docker-compose.yml
# token: netscan-token (default) or your .env value
# org: test-org (default) or your .env value
```

---

## Section 2: Native systemd Deployment (Alternative)

Native deployment provides maximum security by running as a non-root service user with Linux capabilities. Recommended for production environments requiring strict security controls.

### Prerequisites

* Go 1.25+
* InfluxDB 2.x (separate installation)
* Linux with systemd
* Root privileges for installation (service runs as non-root)

### Installation

**1. Install Dependencies**

Ubuntu/Debian:
```bash
sudo apt update
sudo apt install golang-go
```

Arch/CachyOS:
```bash
sudo pacman -S go
```

**2. Clone and Build**
```bash
git clone https://github.com/kljama/netscan.git
cd netscan
go mod download
go build -o netscan ./cmd/netscan
```

**3. Deploy with Automated Script**
```bash
sudo ./deploy.sh
```

This creates:
* `/opt/netscan/` directory with binary, config, and `.env` file
* `netscan` system user (no shell, minimal privileges)
* CAP_NET_RAW capability on binary (via setcap)
* Systemd service unit with security restrictions
* Secure `.env` file (mode 600) for credentials

**4. Configure**

Edit `/opt/netscan/config.yml` with your network ranges:
```yaml
networks:
  - "192.168.1.0/24"    # YOUR actual network
```

Edit `/opt/netscan/.env` with your InfluxDB credentials:
```bash
INFLUXDB_URL=http://localhost:8086
INFLUXDB_TOKEN=<your-influxdb-token>
INFLUXDB_ORG=<your-org>
INFLUXDB_BUCKET=netscan
SNMP_COMMUNITY=<your-snmp-community>
```

**5. Start Service**
```bash
sudo systemctl start netscan
sudo systemctl enable netscan  # Start on boot
```

### Service Management

```bash
# Check status
sudo systemctl status netscan

# View logs
sudo journalctl -u netscan -f

# Restart
sudo systemctl restart netscan

# Stop
sudo systemctl stop netscan

# Disable auto-start
sudo systemctl disable netscan
```

### Manual Deployment (Alternative)

If you prefer not to use `deploy.sh`:

```bash
# Build binary
go build -o netscan ./cmd/netscan

# Create installation directory
sudo mkdir -p /opt/netscan
sudo cp netscan /opt/netscan/
sudo cp config.yml /opt/netscan/

# Set ICMP capability
sudo setcap cap_net_raw+ep /opt/netscan/netscan

# Create service user
sudo useradd -r -s /bin/false netscan
sudo chown -R netscan:netscan /opt/netscan

# Create systemd service
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

# Load environment variables
EnvironmentFile=/opt/netscan/.env

# Security settings
NoNewPrivileges=yes
PrivateTmp=yes
ProtectSystem=strict
AmbientCapabilities=CAP_NET_RAW

[Install]
WantedBy=multi-user.target
EOF

# Reload and start
sudo systemctl daemon-reload
sudo systemctl enable netscan
sudo systemctl start netscan
```

### Security Features

**Native Deployment Security:**
* Runs as dedicated `netscan` service user (not root)
* Service user has `/bin/false` shell (no interactive login)
* CAP_NET_RAW capability via setcap (no root required for ICMP)
* Systemd security restrictions: NoNewPrivileges, PrivateTmp, ProtectSystem=strict
* Credentials in `.env` file with mode 600 (readable only by service user)

**Comparison with Docker:**
* Native: Non-root execution, tighter security controls
* Docker: Root required for ICMP, container isolation provides security boundary

### Command Line Usage

```bash
# Standard mode (uses config.yml in working directory)
./netscan

# Custom config path
./netscan -config /path/to/config.yml

# Help
./netscan -help
```

### Health Check Endpoint

Once running, the service exposes an HTTP server on port 8080 (configurable):

```bash
# Detailed health status
curl http://localhost:8080/health | jq

# Readiness probe (for load balancers)
curl http://localhost:8080/health/ready

# Liveness probe (for process monitors)
curl http://localhost:8080/health/live
```

### Uninstallation

**Automated:**
```bash
sudo ./undeploy.sh
```

**Manual:**
```bash
sudo systemctl stop netscan
sudo systemctl disable netscan
sudo rm /etc/systemd/system/netscan.service
sudo systemctl daemon-reload
sudo rm -rf /opt/netscan
sudo userdel netscan
```

### Troubleshooting

**ICMP permission denied:**
```bash
# Check capability
getcap /opt/netscan/netscan
# Should show: cap_net_raw+ep

# Manually set capability
sudo setcap cap_net_raw+ep /opt/netscan/netscan
```

**InfluxDB connection issues:**
```bash
# Verify InfluxDB is running
systemctl status influxdb

# Test connectivity
curl http://localhost:8086/health

# Check credentials in /opt/netscan/.env
# Review logs
sudo journalctl -u netscan -n 50
```

**No devices discovered:**
```bash
# Verify network ranges in config.yml
# Check firewall rules for ICMP/SNMP
# Test manually
ping 192.168.1.1
# Check logs
sudo journalctl -u netscan | grep discovery
```

---

## Section 3: Configuration Reference

Complete documentation of all configuration parameters. All duration fields accept Go duration format (e.g., "5m", "2s", "1h30m").

### Configuration File Location

* **Docker:** `/app/config.yml` (mounted from host `./config.yml`)
* **Native:** `config.yml` in working directory or via `-config` flag

### Environment Variable Expansion

The configuration file supports `${VAR_NAME}` syntax for environment variable expansion. Variables are loaded from:
* **Docker:** docker-compose.yml environment section or `.env` file
* **Native:** `/opt/netscan/.env` file (loaded by systemd EnvironmentFile)

### Network Discovery Settings

```yaml
# Network ranges to scan (CIDR notation)
# REQUIRED: Update with YOUR actual network ranges
networks:
  - "192.168.1.0/24"    # Example: Home network
  - "10.0.50.0/24"      # Example: Server network
  # Add more ranges as needed
```

**Notes:**
* Use CIDR notation (e.g., `/24` for 254 hosts, `/16` for 65,534 hosts)
* Smaller ranges scan faster
* Maximum /16 recommended (security limit)

```yaml
# How often to run ICMP discovery to find new devices
# Default: "5m"
# Range: Minimum "1m" (enforced by min_scan_interval)
icmp_discovery_interval: "5m"
```

**Notes:**
* More frequent scans find new devices faster but increase CPU/network load
* 5 minutes is reasonable for most networks

```yaml
# Daily scheduled SNMP scan time (HH:MM format, 24-hour)
# Full SNMP scan of all known devices runs once per day at this time
# Default: "02:00" (2:00 AM)
# Set to empty string "" to disable scheduled SNMP scans
snmp_daily_schedule: "02:00"
```

**Notes:**
* Uses local system time
* Immediate SNMP scan still runs on device discovery
* Schedule useful for refreshing metadata overnight

### SNMP Settings

```yaml
snmp:
  # SNMPv2c community string (uses environment variable expansion)
  # Default: "public" (CHANGE THIS for production!)
  community: "${SNMP_COMMUNITY}"
  
  # SNMP port
  # Default: 161
  port: 161
  
  # Timeout for SNMP queries
  # Default: "5s"
  timeout: "5s"
  
  # Number of retries for failed SNMP queries
  # Default: 1
  retries: 1
```

**Notes:**
* SNMPv2c uses plain-text community strings (not secure)
* SNMPv3 support is deferred to future releases
* Increase timeout for slow/distant devices
* Increase retries for unreliable networks

### Monitoring Settings

```yaml
# Ping frequency per monitored device
# Default: "2s"
# Each device gets dedicated pinger goroutine pinging at this interval
ping_interval: "2s"

# Timeout for individual ping operations
# Default: "2s"
ping_timeout: "2s"
```

**Notes:**
* Lower intervals (e.g., "1s") provide more data points but increase CPU/network load
* ping_timeout should be ≤ ping_interval to avoid overlap

### Performance Tuning

```yaml
# Number of concurrent ICMP ping workers for discovery scans
# Default: 1024 (high-performance servers)
# Recommended: 2-4x CPU cores for small deployments, higher for large networks
# Range: 1-2000
icmp_workers: 1024

# Number of concurrent SNMP polling workers
# Default: 256 (high-performance servers)
# Recommended: 1-2x CPU cores (SNMP is CPU-intensive due to protocol parsing)
# Range: 1-1000
snmp_workers: 256
```

**Worker Count Guidelines:**

| System Type       | CPU Cores | ICMP Workers | SNMP Workers | Max Devices | Max Pingers |
|-------------------|-----------|--------------|--------------|-------------|-------------|
| Raspberry Pi      | 4         | 8            | 4            | 100         | 100         |
| Home Server       | 4-8       | 16           | 8            | 500         | 500         |
| Workstation       | 8-16      | 32           | 16           | 1000        | 1000        |
| Small Server      | 8-16      | 128          | 64           | 5000        | 5000        |
| Large Server      | 16+       | 1024         | 256          | 20000       | 20000       |

**Notes:**
* Start conservative and increase based on CPU usage
* Monitor with `htop` or `top`
* Network latency affects optimal worker count

### InfluxDB Settings

```yaml
influxdb:
  # InfluxDB v2 server URL
  # Docker default: "http://localhost:8086" (host network mode)
  # Native default: "http://localhost:8086" (assumes local InfluxDB)
  url: "http://localhost:8086"
  
  # API authentication token (uses environment variable expansion)
  # Docker: Set in docker-compose.yml or .env file
  # Native: Set in /opt/netscan/.env file
  token: "${INFLUXDB_TOKEN}"
  
  # Organization name (uses environment variable expansion)
  # Docker default: "test-org"
  # Native: Your organization name
  org: "${INFLUXDB_ORG}"
  
  # Bucket for metrics storage
  # Default: "netscan"
  bucket: "netscan"
  
  # Bucket for health metrics storage
  # Default: "health"
  health_bucket: "health"
  
  # Batch write settings for performance
  # Number of points to accumulate before writing
  # Default: 5000 (high-performance deployments)
  # Range: 10-10000
  batch_size: 5000
  
  # Maximum time to hold points before flushing
  # Default: "5s"
  # Range: "1s"-"60s"
  flush_interval: "5s"
```

**Batching Notes:**
* Batching reduces InfluxDB requests by ~99% for large deployments
* Larger batch_size reduces request frequency but increases memory usage
* Shorter flush_interval reduces data lag but increases request frequency
* Default (5000 points, 5s) optimized for high-performance servers with 10,000+ devices
* For small deployments (100-1000 devices), consider batch_size: 100

**InfluxDB Schema:**
* Measurement: `ping` (primary bucket)
  * Tags: `ip`, `hostname`
  * Fields: `rtt_ms` (float64), `success` (bool)
* Measurement: `device_info` (primary bucket)
  * Tags: `ip`
  * Fields: `hostname` (string), `snmp_description` (string)
* Measurement: `health_metrics` (health bucket)
  * Tags: none
  * Fields: `device_count` (int), `active_pingers` (int), `goroutines` (int), `memory_mb` (int), `influxdb_ok` (bool), `influxdb_successful_batches` (uint64), `influxdb_failed_batches` (uint64)

### Health Check Endpoint

```yaml
# HTTP server port for health check endpoints
# Default: 8080
# Used by Docker HEALTHCHECK and Kubernetes probes
health_check_port: 8080

# Interval for writing health metrics to InfluxDB health bucket
# Default: "10s"
# Health metrics include device count, memory usage, goroutines, InfluxDB stats
health_report_interval: "10s"
```

**Endpoints:**
* `GET /health` - Detailed JSON status (device count, memory, goroutines, InfluxDB stats)
* `GET /health/ready` - Readiness probe (200 if InfluxDB OK, 503 if unavailable)
* `GET /health/live` - Liveness probe (200 if application running)

**Health Metrics Persistence:**
* Health metrics are automatically written to the health bucket at the configured interval
* Enables long-term tracking of application performance and resource usage

**Docker Integration:**
* HEALTHCHECK directive in Dockerfile uses `/health/live`
* docker-compose.yml healthcheck uses wget on `/health/live`

**Kubernetes Integration:**
* Configure livenessProbe with `/health/live`
* Configure readinessProbe with `/health/ready`

### Resource Protection Settings

```yaml
# Maximum number of concurrent pinger goroutines
# Default: 20000 (high-performance servers)
# Prevents goroutine exhaustion
max_concurrent_pingers: 20000

# Maximum number of devices to monitor
# Default: 20000 (high-performance servers)
# When limit reached, oldest devices (by LastSeen) are evicted (LRU)
max_devices: 20000

# Minimum interval between discovery scans (rate limiting)
# Default: "1m"
# Prevents accidental tight loops
min_scan_interval: "1m"

# Memory usage warning threshold in MB
# Default: 16384 (16GB, high-performance servers)
# Logs warning when exceeded, does not stop service
memory_limit_mb: 16384
```

**Notes:**
* Resource limits prevent accidental DoS and resource exhaustion
* Memory baseline: ~50MB + ~1KB per device
* Adjust limits based on your hardware and network size
* High-performance defaults support up to 20,000 devices with 16GB RAM
* For small deployments, consider: max_devices: 1000, max_concurrent_pingers: 1000, memory_limit_mb: 512

### Complete Example

```yaml
# Network Discovery
networks:
  - "192.168.1.0/24"
  - "10.0.50.0/24"
icmp_discovery_interval: "5m"
snmp_daily_schedule: "02:00"

# SNMP
snmp:
  community: "${SNMP_COMMUNITY}"
  port: 161
  timeout: "5s"
  retries: 1

# Monitoring
ping_interval: "2s"
ping_timeout: "2s"

# Performance
icmp_workers: 1024
snmp_workers: 256

# InfluxDB
influxdb:
  url: "http://localhost:8086"
  token: "${INFLUXDB_TOKEN}"
  org: "${INFLUXDB_ORG}"
  bucket: "netscan"
  batch_size: 5000
  flush_interval: "5s"

# Health Check
health_check_port: 8080

# Resource Limits
max_concurrent_pingers: 20000
max_devices: 20000
min_scan_interval: "1m"
memory_limit_mb: 16384
```

### Environment Variables Reference

These environment variables are used in config.yml via `${VAR_NAME}` syntax:

**Docker Deployment (via docker-compose.yml or .env file):**
```bash
INFLUXDB_TOKEN=netscan-token              # InfluxDB API token
INFLUXDB_ORG=test-org                     # InfluxDB organization
SNMP_COMMUNITY=public                     # SNMPv2c community string
DOCKER_INFLUXDB_INIT_USERNAME=admin       # InfluxDB admin username
DOCKER_INFLUXDB_INIT_PASSWORD=admin123    # InfluxDB admin password
DOCKER_INFLUXDB_INIT_ADMIN_TOKEN=netscan-token  # Same as INFLUXDB_TOKEN
```

**Native Deployment (via /opt/netscan/.env file):**
```bash
INFLUXDB_URL=http://localhost:8086        # InfluxDB server URL
INFLUXDB_TOKEN=<your-token>               # InfluxDB API token
INFLUXDB_ORG=<your-org>                   # InfluxDB organization
INFLUXDB_BUCKET=netscan                   # InfluxDB bucket
SNMP_COMMUNITY=<your-community>           # SNMPv2c community string
```

**Security Best Practices:**
* Never commit `.env` files to version control
* Use `chmod 600 .env` to restrict permissions
* Generate strong, unique tokens: `openssl rand -base64 32`
* Change SNMP community from default "public"
* Rotate credentials regularly

---

## License

MIT License - See LICENSE.md
