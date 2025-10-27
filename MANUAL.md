# netscan - Complete Manual

This manual provides comprehensive documentation for netscan, a production-grade network monitoring service.

**Contents:**
* **Part I: Deployment Guide** - Complete deployment instructions for Docker and Native deployments
* **Part II: Development Guide** - Architecture, development setup, building, testing, and contributing *(Coming in next update)*
* **Part III: Reference Documentation** - Configuration, API reference, and file structure *(Coming in next update)*

---

# Part I: Deployment Guide

## Overview

netscan is a production-grade Go network monitoring service that performs automated network device discovery and continuous uptime monitoring. The service operates through a multi-ticker event-driven architecture with five independent monitoring workflows:

1. **ICMP Discovery** - Periodic network sweeps to find responsive devices
2. **SNMP Enrichment** - Scheduled metadata collection from discovered devices
3. **Continuous Monitoring** - Per-device ICMP ping monitoring with rate limiting
4. **Pinger Reconciliation** - Automatic lifecycle management of monitoring goroutines
5. **State Pruning** - Removal of stale devices

All discovered devices are stored in a central StateManager (single source of truth), and all metrics are written to InfluxDB v2 using an optimized batching system.

**Deployment Options:**
- **Docker Deployment (Recommended)** - Easiest path with automatic orchestration
- **Native systemd Deployment (Alternative)** - Maximum security with capability-based isolation

---

## Section 1: Docker Deployment (Recommended)

Docker deployment provides the easiest path to get netscan running with automatic orchestration of the complete stack (netscan + InfluxDB).

### Prerequisites

* **Docker Engine** 20.10 or later
* **Docker Compose** V2 (comes with Docker Desktop or install separately)
* **Network access** to target devices for ICMP and SNMP
* **Host network access** (for ICMP raw sockets - see Architecture Notes below)

### Installation Steps

#### 1. Clone Repository

```bash
git clone https://github.com/kljama/netscan.git
cd netscan
```

#### 2. Create Configuration File

```bash
cp config.yml.example config.yml
```

**CRITICAL:** Edit `config.yml` and update the `networks` section with your actual network ranges:

```yaml
networks:
  - "192.168.1.0/24"    # YOUR actual network range
  - "10.0.50.0/24"      # Add additional ranges as needed
```

⚠️ **Important:** The example networks (192.168.0.0/24) are placeholders. If these don't match your network, netscan will find 0 devices. Use `ip addr` (Linux) or `ipconfig` (Windows) to determine your network range.

#### 3. Configure Credentials (Optional but Recommended for Production)

For production security, create a `.env` file to override default credentials:

```bash
cp .env.example .env
chmod 600 .env
```

Edit `.env` and set secure values:

```bash
# InfluxDB Token (generate with: openssl rand -base64 32)
INFLUXDB_TOKEN=<your-secure-token>
DOCKER_INFLUXDB_INIT_ADMIN_TOKEN=<same-as-INFLUXDB_TOKEN>

# InfluxDB Admin Password
DOCKER_INFLUXDB_INIT_PASSWORD=<strong-password>

# SNMP Community String (change from default 'public')
SNMP_COMMUNITY=<your-snmp-community>
```

The `.env` file is automatically loaded by Docker Compose. Variables are expanded in `config.yml` using syntax like `${INFLUXDB_TOKEN}`.

**Default credentials (for testing only):**
- InfluxDB Token: `netscan-token`
- InfluxDB Admin: `admin` / `admin123`
- SNMP Community: `public`

#### 4. Start the Stack

```bash
docker compose up -d
```

This command:
- Builds the netscan Docker image from the local Dockerfile (multi-stage build)
- Starts InfluxDB v2.7 container with automatic initialization
- Starts netscan container with health checks
- Creates persistent volume for InfluxDB data

#### 5. Verify Operation

```bash
# Check container status (both should be 'Up' and 'healthy')
docker compose ps

# View netscan logs in real-time
docker compose logs -f netscan

# Check health endpoint (requires jq for pretty JSON)
curl http://localhost:8080/health | jq

# Alternative: check without jq
curl http://localhost:8080/health
```

Expected output from health endpoint:
```json
{
  "status": "healthy",
  "version": "1.0.0",
  "uptime": "5m30s",
  "device_count": 15,
  "active_pingers": 15,
  "influxdb_ok": true,
  ...
}
```

#### 6. Access InfluxDB UI (Optional)

Navigate to http://localhost:8086 in your browser:
- **Username:** `admin`
- **Password:** `admin123` (or your `.env` value)
- **Organization:** `test-org`
- **Primary Bucket:** `netscan` (ping results and device info)
- **Health Bucket:** `health` (application metrics)

### Service Management

```bash
# Stop services (keeps data volumes)
docker compose stop

# Start services again
docker compose start

# Restart services (useful after config changes)
docker compose restart netscan

# View logs for specific service
docker compose logs -f netscan
docker compose logs -f influxdb

# Stop and remove containers (keeps volumes)
docker compose down

# Stop and remove containers + volumes (DELETES ALL DATA)
docker compose down -v

# Rebuild and restart after code changes
docker compose up -d --build
```

### Docker Architecture Notes

#### Why `network_mode: host`?

The netscan service uses `network_mode: host` in `docker-compose.yml` to access the host's network stack directly. This is **required** for two reasons:

1. **ICMP Raw Sockets:** ICMP ping requires raw socket access, which needs direct access to the host network interfaces
2. **Network Discovery:** To discover devices on local subnets (192.168.x.x, 10.x.x.x), netscan needs to see the actual network topology

**Trade-off:** The container shares the host's network namespace, so port 8080 (health check) is exposed on the host. This is acceptable for a monitoring service but means you cannot run multiple netscan instances on the same host.

#### Why `cap_add: NET_RAW`?

The `NET_RAW` capability grants permission to create raw ICMP sockets. This is defined in `docker-compose.yml`:

```yaml
cap_add:
  - NET_RAW
```

The Dockerfile also sets this capability on the binary:
```dockerfile
RUN setcap cap_net_raw+ep /app/netscan
```

**Security Note:** Even with `CAP_NET_RAW` capability, the container runs as `root` user. This is a Linux kernel limitation - non-root users cannot create raw ICMP sockets in Docker containers despite capability grants. This is documented in the Dockerfile (lines 48-51) as an accepted security trade-off for ICMP functionality.

#### Log Rotation

Docker Compose configures automatic log rotation to prevent disk space exhaustion:

```yaml
logging:
  driver: json-file
  options:
    max-size: "10m"  # Maximum size of a single log file
    max-file: "3"    # Keep 3 most recent log files (~30MB total)
```

This ensures logs don't grow indefinitely while preserving recent history for debugging.

#### Health Checks

Both services have health checks configured:

**InfluxDB Health Check:**
```yaml
healthcheck:
  test: ["CMD", "influx", "ping"]
  interval: 10s
  timeout: 5s
  retries: 5
  start_period: 30s
```

**netscan Health Check:**
```yaml
healthcheck:
  test: ["CMD", "wget", "--spider", "-q", "http://localhost:8080/health/live"]
  interval: 30s
  timeout: 3s
  retries: 3
  start_period: 40s
```

The netscan container waits for InfluxDB to be healthy before starting:
```yaml
depends_on:
  influxdb:
    condition: service_healthy
```

### Troubleshooting

#### Issue: "0 devices found" in logs

**Cause:** Network ranges in `config.yml` don't match your actual network.

**Solution:**
1. Find your network range: `ip addr` (Linux) or `ipconfig` (Windows)
2. Update `networks` in `config.yml` with correct CIDR notation
3. Restart: `docker compose restart netscan`

**Example:** If your IP is `192.168.1.50` with subnet mask `255.255.255.0`, use `192.168.1.0/24`

#### Issue: "InfluxDB connection failed" on startup

**Cause:** InfluxDB not ready or credentials mismatch.

**Solution:**
1. Check InfluxDB is healthy: `docker compose ps` (should show "healthy")
2. Check InfluxDB logs: `docker compose logs influxdb`
3. Verify token in `.env` matches between `INFLUXDB_TOKEN` and `DOCKER_INFLUXDB_INIT_ADMIN_TOKEN`
4. If token changed, recreate containers: `docker compose down -v && docker compose up -d`

#### Issue: Health check endpoint returns 503 "NOT READY"

**Cause:** Service started but InfluxDB connectivity failing.

**Solution:**
1. Check `/health/ready` endpoint: `curl http://localhost:8080/health/ready`
2. Check `/health` for details: `curl http://localhost:8080/health | jq .influxdb_ok`
3. Verify InfluxDB is accessible: `curl http://localhost:8086/health`
4. Check network connectivity between containers

#### Issue: Permission denied errors for ICMP

**Cause:** Container doesn't have NET_RAW capability or not running as root.

**Solution:**
1. Verify capability in docker-compose.yml: `cap_add: - NET_RAW`
2. Check container is running as root (this is required, not a bug)
3. Restart containers: `docker compose restart netscan`

#### Issue: High memory usage

**Cause:** Monitoring too many devices or rate limits too high.

**Solution:**
1. Check device count: `curl http://localhost:8080/health | jq .device_count`
2. Reduce network ranges in `config.yml`
3. Lower `ping_rate_limit` and `ping_burst_limit` in `config.yml`
4. Increase `memory_limit_mb` if devices are legitimate
5. Restart: `docker compose restart netscan`

#### Issue: Containers exit immediately

**Cause:** Configuration error or missing files.

**Solution:**
1. Check logs: `docker compose logs netscan`
2. Verify `config.yml` exists and is valid YAML
3. Ensure `.env` file has no syntax errors
4. Try starting in foreground: `docker compose up` (without `-d`)

### Cleaning Up

To completely remove netscan and all data:

```bash
# Stop and remove all containers and volumes
docker compose down -v

# Remove Docker images
docker rmi netscan:latest influxdb:2.7

# Remove any orphaned volumes
docker volume prune
```

---

## Section 2: Native systemd Deployment (Alternative)

Native systemd deployment provides maximum security through capability-based isolation and dedicated system users. This is the recommended deployment for security-conscious production environments.

### Prerequisites

* **Go** 1.25 or later
* **InfluxDB** v2.x running and accessible (local or remote)
* **systemd** (most modern Linux distributions)
* **libcap** package for setcap command
* **Root/sudo access** for installation

### Verifying Prerequisites

```bash
# Check Go version (should be 1.25+)
go version

# Check systemd
systemctl --version

# Check if setcap is available
which setcap

# Verify InfluxDB is running (if local)
curl http://localhost:8086/health
```

### Installation Using deploy.sh

The `deploy.sh` script automates the entire installation process with proper security hardening.

#### 1. Clone and Prepare

```bash
git clone https://github.com/kljama/netscan.git
cd netscan
```

#### 2. Configure Application

```bash
# Copy configuration template
cp config.yml.example config.yml

# Edit configuration with your network ranges and InfluxDB details
nano config.yml  # or vim, vi, etc.
```

**Required changes in `config.yml`:**
- `networks`: Your actual CIDR ranges
- `influxdb.url`: InfluxDB server URL (e.g., `http://localhost:8086`)
- `influxdb.token`: Use `${INFLUXDB_TOKEN}` for environment variable expansion
- `snmp.community`: Use `${SNMP_COMMUNITY}` for environment variable expansion

#### 3. Run Deployment Script

```bash
sudo ./deploy.sh
```

**What the script does:**

1. **Go Version Check:** Verifies Go 1.21+ is installed
2. **Binary Build:** Compiles netscan binary from source
3. **Service User Creation:** Creates dedicated `netscan` system user
   - System account (UID < 1000)
   - No shell access (`/bin/false`)
   - No home directory
   - Cannot login
4. **File Installation:**
   - Creates `/opt/netscan/` directory
   - Installs binary to `/opt/netscan/netscan`
   - Copies `config.yml` to `/opt/netscan/config.yml`
   - Creates `/opt/netscan/.env` with secure environment variables
5. **Permission Setting:**
   - Binary: `755` (executable)
   - .env file: `600` (owner read/write only)
   - Ownership: `netscan:netscan`
6. **Capability Grant:** Sets `cap_net_raw+ep` on binary for ICMP access
7. **systemd Service Creation:** Installs and enables service
8. **Service Start:** Starts netscan service immediately

**Expected output:**
```
[INFO] Go 1.25.1 found ✓
[INFO] Building netscan binary...
[INFO] Binary built successfully ✓
[INFO] Creating service user: netscan
[INFO] Service user created successfully ✓
[INFO] Installing files to /opt/netscan
[INFO] .env file created with secure placeholders ✓
[INFO] Files installed successfully ✓
[INFO] Setting ownership and permissions
[INFO] .env file permissions set to 600 ✓
[INFO] Permissions set successfully ✓
[INFO] Setting CAP_NET_RAW capability for ICMP access
[INFO] Capabilities set successfully ✓
[INFO] Creating systemd service
[INFO] Systemd service created ✓
[INFO] Enabling and starting systemd service
[INFO] Service enabled and started successfully ✓
[INFO] netscan deployed and running as a systemd service
```

#### 4. Configure Environment Variables

Edit `/opt/netscan/.env` with your actual credentials:

```bash
sudo nano /opt/netscan/.env
```

**Required values:**
```bash
# InfluxDB credentials
INFLUXDB_TOKEN=your-actual-influxdb-token
INFLUXDB_ORG=your-org-name

# SNMP community string
SNMP_COMMUNITY=your-snmp-community
```

After editing, restart the service:
```bash
sudo systemctl restart netscan
```

### Security Model

The native deployment provides significantly better security than Docker:

#### 1. Dedicated System User

```bash
# Created by deploy.sh
useradd -r -s /bin/false netscan
```

- `-r`: System account (non-interactive, UID < 1000)
- `-s /bin/false`: Prevents shell login
- No password set (cannot login)
- Principle of least privilege

#### 2. Capability-Based Security

Instead of running as root, the binary is granted only the specific capability it needs:

```bash
# Applied by deploy.sh
setcap cap_net_raw+ep /opt/netscan/netscan
```

- `cap_net_raw`: Allows raw ICMP socket creation
- `+ep`: Effective and Permitted flags
- Capability persists across executions
- Much safer than full root privileges

You can verify the capability:
```bash
getcap /opt/netscan/netscan
# Output: /opt/netscan/netscan = cap_net_raw+ep
```

#### 3. systemd Service Hardening

The generated systemd service (`/etc/systemd/system/netscan.service`) includes multiple security hardening directives:

```ini
[Service]
Type=simple
User=netscan
Group=netscan
ExecStart=/opt/netscan/netscan
WorkingDirectory=/opt/netscan

# Environment variables from secure file
EnvironmentFile=/opt/netscan/.env

# Security hardening
NoNewPrivileges=yes          # Prevents privilege escalation
PrivateTmp=yes               # Isolated /tmp directory
ProtectSystem=strict         # Read-only filesystem except /opt/netscan
AmbientCapabilities=CAP_NET_RAW  # Only grant needed capability
```

#### 4. Secure Credential Storage

The `.env` file is protected:
- Permissions: `600` (owner read/write only)
- Owner: `netscan:netscan`
- Contains sensitive tokens and credentials
- Automatically loaded by systemd via `EnvironmentFile` directive
- Not readable by other users

**Comparison with Docker:**

| Security Aspect | Native systemd | Docker |
|----------------|----------------|---------|
| User privileges | Dedicated non-root user | root (required) |
| Capability model | Single capability (CAP_NET_RAW) | Full CAP_NET_RAW |
| Filesystem | ProtectSystem=strict | Container isolation |
| Shell access | /bin/false (disabled) | N/A |
| Tmp isolation | PrivateTmp=yes | N/A |
| Privilege escalation | NoNewPrivileges=yes | N/A |

### Service Management

#### Start/Stop/Restart

```bash
# Start service
sudo systemctl start netscan

# Stop service
sudo systemctl stop netscan

# Restart service (after config changes)
sudo systemctl restart netscan

# Check if service is running
sudo systemctl is-active netscan
```

#### Enable/Disable Auto-Start

```bash
# Enable auto-start on boot (done by deploy.sh)
sudo systemctl enable netscan

# Disable auto-start
sudo systemctl disable netscan

# Check if enabled
sudo systemctl is-enabled netscan
```

#### View Status

```bash
# Detailed status with recent log entries
sudo systemctl status netscan

# Example output:
● netscan.service - netscan network monitoring service
     Loaded: loaded (/etc/systemd/system/netscan.service; enabled)
     Active: active (running) since Mon 2024-01-15 10:30:45 UTC; 2h ago
   Main PID: 1234 (netscan)
      Tasks: 25
     Memory: 45.2M
        CPU: 1min 30s
     CGroup: /system.slice/netscan.service
             └─1234 /opt/netscan/netscan
```

#### View Logs

```bash
# Follow logs in real-time (recommended)
sudo journalctl -u netscan -f

# View last 100 lines
sudo journalctl -u netscan -n 100

# View logs since last boot
sudo journalctl -u netscan -b

# View logs from specific time
sudo journalctl -u netscan --since "1 hour ago"
sudo journalctl -u netscan --since "2024-01-15 10:00:00"

# View logs with priority level (errors only)
sudo journalctl -u netscan -p err

# Export logs to file
sudo journalctl -u netscan > netscan.log
```

#### Configuration Changes

After modifying `/opt/netscan/config.yml` or `/opt/netscan/.env`:

```bash
# Restart to apply changes
sudo systemctl restart netscan

# Verify service restarted successfully
sudo systemctl status netscan

# Check logs for errors
sudo journalctl -u netscan -f
```

### Uninstallation Using undeploy.sh

The `undeploy.sh` script safely removes netscan and all associated files:

```bash
sudo ./undeploy.sh
```

**What the script does:**

1. **Stop Service:** Gracefully stops running service
2. **Disable Service:** Removes from auto-start
3. **Remove Service File:** Deletes `/etc/systemd/system/netscan.service`
4. **Reload systemd:** Updates systemd daemon
5. **Remove Capabilities:** Clears capabilities from binary
6. **Delete Installation Directory:** Removes `/opt/netscan/` and all contents
7. **Remove Service User:** Deletes `netscan` system user
8. **Verify Cleanup:** Confirms complete removal

**Expected output:**
```
[INFO] Stopping and disabling netscan service
[INFO] Service stopped ✓
[INFO] Service disabled ✓
[INFO] Removing systemd service file
[INFO] Service file removed ✓
[INFO] Systemd daemon reloaded ✓
[INFO] Removing capabilities from binary
[INFO] Capabilities removed ✓
[INFO] Removing installation directory: /opt/netscan
[INFO] Installation directory removed (45M) ✓
[INFO] Removing service user: netscan
[INFO] Service user removed ✓
[INFO] No additional artifacts found ✓
[INFO] Complete removal verified ✓
[INFO] netscan has been completely uninstalled
```

### Manual Installation (Advanced)

If you prefer manual installation or need to customize:

```bash
# 1. Build binary
go build -o netscan ./cmd/netscan

# 2. Create user
sudo useradd -r -s /bin/false netscan

# 3. Create installation directory
sudo mkdir -p /opt/netscan

# 4. Install files
sudo cp netscan /opt/netscan/
sudo cp config.yml /opt/netscan/
sudo cp .env.example /opt/netscan/.env

# 5. Set permissions
sudo chown -R netscan:netscan /opt/netscan
sudo chmod 755 /opt/netscan/netscan
sudo chmod 600 /opt/netscan/.env

# 6. Set capability
sudo setcap cap_net_raw+ep /opt/netscan/netscan

# 7. Create systemd service (see deploy.sh for template)
sudo nano /etc/systemd/system/netscan.service

# 8. Enable and start
sudo systemctl daemon-reload
sudo systemctl enable netscan
sudo systemctl start netscan
```

### Troubleshooting

#### Issue: "permission denied" when creating raw socket

**Cause:** Binary doesn't have CAP_NET_RAW capability.

**Solution:**
```bash
# Check current capabilities
getcap /opt/netscan/netscan

# If missing, set capability
sudo setcap cap_net_raw+ep /opt/netscan/netscan

# Restart service
sudo systemctl restart netscan
```

#### Issue: Service fails to start

**Cause:** Configuration error or permission issue.

**Solution:**
```bash
# Check service status
sudo systemctl status netscan

# View detailed logs
sudo journalctl -u netscan -n 50

# Common issues:
# - config.yml syntax error: Validate YAML
# - InfluxDB unreachable: Check URL and network
# - Permission issue: Verify ownership is netscan:netscan
```

#### Issue: "0 devices found"

**Cause:** Network ranges don't match actual network.

**Solution:**
```bash
# Edit config
sudo nano /opt/netscan/config.yml

# Update networks section
networks:
  - "your-actual-network/24"

# Restart
sudo systemctl restart netscan
```

#### Issue: InfluxDB connection failed

**Cause:** Wrong credentials or InfluxDB not accessible.

**Solution:**
```bash
# Check InfluxDB is running
curl http://localhost:8086/health

# Verify token in .env file
sudo cat /opt/netscan/.env

# Test connectivity
curl -H "Authorization: Token YOUR_TOKEN" \
  http://localhost:8086/api/v2/buckets

# Update .env if needed
sudo nano /opt/netscan/.env

# Restart
sudo systemctl restart netscan
```

#### Issue: High CPU or memory usage

**Cause:** Monitoring too many devices or aggressive intervals.

**Solution:**
```bash
# Check metrics
curl http://localhost:8080/health

# Adjust config.yml:
# - Increase ping_interval
# - Reduce networks scope
# - Lower icmp_workers/snmp_workers
# - Adjust ping_rate_limit

sudo nano /opt/netscan/config.yml
sudo systemctl restart netscan
```

### Maintenance

#### Updating netscan

```bash
# 1. Stop service
sudo systemctl stop netscan

# 2. Backup current binary and config
sudo cp /opt/netscan/netscan /opt/netscan/netscan.backup
sudo cp /opt/netscan/config.yml /opt/netscan/config.yml.backup

# 3. Pull latest code
cd /path/to/netscan
git pull origin main

# 4. Rebuild
go build -o netscan ./cmd/netscan

# 5. Install new binary
sudo cp netscan /opt/netscan/

# 6. Reset capability (lost during copy)
sudo setcap cap_net_raw+ep /opt/netscan/netscan

# 7. Check for config changes
diff config.yml.example /opt/netscan/config.yml

# 8. Update config if needed
sudo nano /opt/netscan/config.yml

# 9. Restart
sudo systemctl start netscan

# 10. Verify
sudo systemctl status netscan
sudo journalctl -u netscan -f
```

#### Log Rotation

systemd journal handles log rotation automatically, but you can configure retention:

```bash
# Check current journal size
sudo journalctl --disk-usage

# Configure retention in /etc/systemd/journald.conf:
# SystemMaxUse=1G
# SystemKeepFree=2G
# MaxRetentionSec=1month

# Manually clean old logs
sudo journalctl --vacuum-time=7d
sudo journalctl --vacuum-size=500M
```

---

**End of Part I: Deployment Guide**

*Part II (Development Guide) and Part III (Reference Documentation) will be added in subsequent updates.*

---

# Part II: Development Guide

## Overview

This section is for developers who want to understand the netscan architecture, set up a development environment, build from source, run tests, and contribute to the project.

netscan is built with production-grade concurrency patterns, comprehensive error handling, and strict architectural principles. Understanding these foundations is essential for effective development.

---

## 1. Architecture Overview

### Multi-Ticker Event-Driven Design

netscan implements a sophisticated event-driven architecture with five independent, concurrent monitoring workflows orchestrated in `cmd/netscan/main.go`. All tickers run within a single `select` statement in the main event loop and are controlled by a shared context for coordinated graceful shutdown.

**The Five Tickers:**

#### 1. ICMP Discovery Ticker (`icmpDiscoveryTicker`)

**Interval:** Configurable via `cfg.IcmpDiscoveryInterval` (e.g., `5m`)

**Purpose:** Periodically scans configured network ranges to discover responsive devices

**Operation Flow:**
1. Calls `discovery.RunICMPSweep()` with context, networks, worker count, and rate limiter
2. Returns list of IPs that responded to ICMP echo requests
3. For each responsive IP, calls `stateMgr.AddDevice(ip)` to add to state
4. If device is new (`isNew == true`), launches background goroutine to perform immediate SNMP scan
5. SNMP results written to StateManager via `stateMgr.UpdateDeviceSNMP()`
6. Device info written to InfluxDB via `writer.WriteDeviceInfo()`

**Concurrency:** SNMP scans for new devices run in background goroutines with panic recovery to avoid blocking the discovery loop

**Memory Protection:** Calls `checkMemoryUsage()` before each scan to warn if memory exceeds configured limit

#### 2. Daily SNMP Scan Ticker (`dailySNMPChan`)

**Schedule:** Configurable via `cfg.SNMPDailySchedule` in HH:MM format (e.g., `"02:00"`)

**Purpose:** Performs full SNMP scan of all known devices at a scheduled time each day

**Operation Flow:**
1. Retrieves all device IPs from StateManager via `stateMgr.GetAllIPs()`
2. Calls `discovery.RunSNMPScan()` with all IPs and SNMP configuration
3. Updates StateManager with hostname and sysDescr via `stateMgr.UpdateDeviceSNMP()`
4. Writes device info to InfluxDB via `writer.WriteDeviceInfo()`
5. Logs success and failure counts for visibility

**Implementation:** Uses `createDailySNMPChannel()` function that spawns a goroutine calculating time until next scheduled run with 24-hour wraparound handling

**Optional:** Disabled if `cfg.SNMPDailySchedule` is empty string (creates dummy channel that never fires)

#### 3. Pinger Reconciliation Ticker (`reconciliationTicker`)

**Interval:** Fixed 5 seconds

**Purpose:** Ensures every device in StateManager has an active continuous pinger goroutine

**Operation Flow:**
1. Acquires `pingersMu` lock for thread-safe access to `activePingers` and `stoppingPingers` maps
2. Retrieves current IPs from StateManager and builds lookup map
3. **Start New Pingers:**
   - For each IP in StateManager not in `activePingers` AND not in `stoppingPingers`
   - Respects `cfg.MaxConcurrentPingers` limit (logs warning if reached)
   - Creates child context with `context.WithCancel()`
   - Stores cancel function in `activePingers[ip]`
   - Increments `pingerWg` before starting goroutine
   - Launches wrapper goroutine that calls `monitoring.StartPinger()` and notifies `pingerExitChan` on completion
4. **Stop Removed Pingers:**
   - For each IP in `activePingers` not in current StateManager IPs (device was pruned)
   - Moves IP to `stoppingPingers[ip] = true` before calling cancel function
   - Removes IP from `activePingers` map
   - Calls `cancelFunc()` to signal pinger to stop (asynchronous)
5. Releases `pingersMu` lock

**Race Prevention:** The `stoppingPingers` map prevents race condition where a device is pruned and quickly re-discovered before old pinger fully exits. A separate goroutine listens on `pingerExitChan` and removes IPs from `stoppingPingers` when pingers confirm exit.

**Concurrency Safety:** All map access protected by `pingersMu` mutex

#### 4. State Pruning Ticker (`pruningTicker`)

**Interval:** Fixed 1 hour

**Purpose:** Removes devices that haven't been seen (successful ping) in the last 24 hours

**Operation Flow:**
1. Calls `stateMgr.PruneStale(24 * time.Hour)`
2. Returns list of pruned devices
3. Logs each pruned device at debug level with IP and hostname
4. Logs summary at info level if any devices were pruned

**Integration:** Reconciliation ticker automatically detects removed devices and stops their pingers in next cycle (within 5 seconds)

#### 5. Health Report Ticker (`healthReportTicker`)

**Interval:** Configurable via `cfg.HealthReportInterval` (default: `10s`)

**Purpose:** Writes application health and observability metrics to InfluxDB health bucket

**Operation Flow:**
1. Calls `healthServer.GetHealthMetrics()` to collect current metrics
2. Loads `totalPingsSent` atomic counter value
3. Calls `writer.WriteHealthMetrics()` with:
   - Device count
   - Active pinger count (from `currentInFlightPings.Load()`)
   - Total goroutines
   - Heap memory MB
   - RSS memory MB
   - Suspended device count
   - InfluxDB connectivity status
   - Successful/failed batch counts
   - Total pings sent

### Core Components

#### StateManager: The Single Source of Truth

**Location:** `internal/state/manager.go`

The StateManager is the **central device registry** and the only authoritative source for device state. All components must interact with devices through StateManager's thread-safe methods.

**Thread Safety:**
- `sync.RWMutex` (`mu` field) protects all map operations
- Read operations use `RLock()`/`RUnlock()` for concurrent read access
- Write operations use `Lock()`/`Unlock()` for exclusive write access

**Device Storage:**
- `devices map[string]*Device` - maps IP addresses to device pointers
- `maxDevices int` - enforces device count limit with LRU eviction

**Device Struct Fields:**
```go
type Device struct {
    IP               string    // IPv4 address (map key)
    Hostname         string    // From SNMP or IP as fallback
    SysDescr         string    // SNMP sysDescr MIB-II value
    LastSeen         time.Time // Last successful ping timestamp
    ConsecutiveFails int       // Circuit breaker counter
    SuspendedUntil   time.Time // Circuit breaker suspension timestamp
}
```

**Circuit Breaker Logic:**

The StateManager implements automatic device suspension to prevent wasting resources on consistently failing devices:

- **`ReportPingFail(ip, maxFails, backoff)`**: Increments `ConsecutiveFails` counter. When threshold reached, sets `SuspendedUntil` to `now + backoff` and returns `true` to indicate suspension.

- **`IsSuspended(ip)`**: Returns `true` if `SuspendedUntil` is set and in the future. Pingers check this before acquiring rate limiter token.

- **`ReportPingSuccess(ip)`**: Resets `ConsecutiveFails` to 0 and clears `SuspendedUntil` on any successful ping.

**LRU Eviction:**
- Triggered when `len(devices) >= maxDevices`
- Iterates all devices to find oldest `LastSeen` timestamp
- Deletes device with smallest (oldest) `LastSeen` time
- Trade-off: O(n) eviction time for simplicity (no heap/priority queue)

**Key Methods:**
- `AddDevice(ip)` - Add by IP only, returns true if new
- `UpdateDeviceSNMP(ip, hostname, sysDescr)` - Enrich with SNMP data
- `UpdateLastSeen(ip)` - Update timestamp on successful ping
- `GetAllIPs()` - Used by reconciliation and daily SNMP scan
- `PruneStale(olderThan)` - Remove devices not seen within duration
- `GetSuspendedCount()` - Count for health metrics

#### InfluxDB Writer: High-Performance Batching

**Location:** `internal/influx/writer.go`

The writer implements a sophisticated lock-free batching system for high-throughput metric writes.

**Architecture:**
- **Channel-based design:** `batchChan chan *write.Point` (buffered: 2x batch size)
- **Background flusher:** Single goroutine accumulates and flushes points
- **Dual-bucket:** Separate WriteAPIs for primary metrics and health metrics

**Batching Logic:**
1. Points added to channel via non-blocking send (drops on full, logs warning)
2. Background flusher accumulates in local slice (no mutex needed)
3. Size-based flush: When batch reaches `batchSize` points
4. Time-based flush: Every `flushInterval` even if batch incomplete
5. Retry logic: Up to 3 attempts with exponential backoff (1s, 2s, 4s)

**Write Methods:**
- `WritePingResult(ip, rtt, successful)` - Measurement: `"ping"`, Tags: `ip`, Fields: `rtt_ms`, `success`
- `WriteDeviceInfo(ip, hostname, sysDescr)` - Measurement: `"device_info"`, Tags: `ip`, Fields: `hostname`, `snmp_description`
- `WriteHealthMetrics(...)` - Measurement: `"health_metrics"`, bypasses batch channel for direct write

**Data Sanitization:**
- `sanitizeInfluxString()` - Length limiting (500 chars), control character removal, whitespace trimming
- Applied to hostname and sysDescr to prevent database corruption

**Metrics Tracking:**
- `successfulBatches atomic.Uint64` - Successful batch writes
- `failedBatches atomic.Uint64` - Failed batch writes
- Exposed via `GetSuccessfulBatches()` and `GetFailedBatches()` for health reporting

#### Continuous Monitoring: Per-Device Pingers

**Location:** `internal/monitoring/pinger.go`

Each discovered device gets a dedicated pinger goroutine that runs continuously until the device is removed or service shuts down.

**Lifecycle:**
1. Created by reconciliation ticker when device added to StateManager
2. Runs in infinite loop with `time.Timer` for interval-based pings
3. Defers `wg.Done()` to signal completion on exit
4. Includes panic recovery to prevent single pinger crash from affecting service

**Operation Sequence Per Ping:**
1. **Circuit Breaker Check:** Calls `stateMgr.IsSuspended(ip)` before acquiring rate limiter token (skips ping if suspended)
2. **Rate Limiting:** Acquires token from global rate limiter via `limiter.Wait(ctx)` (blocks until available or cancelled)
3. **Ping Execution:** Calls `performPingWithCircuitBreaker()` which:
   - Increments `inFlightCounter` (atomic) at start
   - Decrements on completion (defer)
   - Increments `totalPingsSent` (atomic) for observability
   - Validates IP address (rejects loopback, multicast, link-local)
   - Creates pinger with `probing.NewPinger(ip)`
   - Executes single ICMP echo request with configured timeout
   - Determines success by `len(stats.Rtts) > 0 && stats.AvgRtt > 0`
4. **State Updates on Success:**
   - `stateMgr.ReportPingSuccess(ip)` - Resets circuit breaker
   - `stateMgr.UpdateLastSeen(ip)` - Updates timestamp
5. **State Updates on Failure:**
   - `stateMgr.ReportPingFail(ip, maxFails, backoff)` - Increments counter, suspends if threshold reached
   - Logs warning when circuit breaker trips
6. **Metrics Writing:** `writer.WritePingResult(ip, rtt, success)`
7. **Timer Reset:** Schedules next ping after interval (time between pings, not fixed schedule)

**Interface Design:**
```go
type PingWriter interface {
    WritePingResult(ip string, rtt time.Duration, successful bool) error
    WriteDeviceInfo(ip, hostname, sysDescr string) error
}

type StateManager interface {
    UpdateLastSeen(ip string)
    ReportPingSuccess(ip string)
    ReportPingFail(ip string, maxFails int, backoff time.Duration) bool
    IsSuspended(ip string) bool
}
```

These interfaces enable easy mocking in unit tests.

### Concurrency Model

netscan implements comprehensive concurrency safety through multiple mechanisms:

**1. Context-Based Cancellation:**
- Main context created with `context.WithCancel(context.Background())`
- All child operations receive contexts derived from main context
- Signal handler (SIGINT, SIGTERM) calls `stop()` which cancels main context
- Context cancellation propagates to all goroutines, triggering coordinated shutdown

**2. WaitGroup Tracking (`pingerWg`):**
- Tracks all pinger goroutines for graceful shutdown
- `pingerWg.Add(1)` called before starting each pinger wrapper goroutine
- `defer pingerWg.Done()` in `monitoring.StartPinger()` ensures count decremented on exit
- Shutdown sequence calls `pingerWg.Wait()` to block until all pingers confirm exit

**3. Mutex Protection (`pingersMu`):**
- `sync.Mutex` protects concurrent access to:
  - `activePingers` map (IP string → context.CancelFunc)
  - `stoppingPingers` map (IP string → bool)
- Locked during reconciliation loop when starting/stopping pingers
- Locked when removing IPs from `stoppingPingers` via exit notification handler

**4. Atomic Counters:**
- `currentInFlightPings atomic.Int64` - Tracks active pinger count for accurate observability
- `totalPingsSent atomic.Uint64` - Tracks cumulative pings sent across all devices
- Lock-free atomic operations for high-frequency updates without contention

**5. Panic Recovery:**
- All long-running goroutines wrapped with `defer func() { recover() }` pattern
- Includes: discovery workers, SNMP scan workers, pingers, shutdown handler, daily SNMP scheduler, pinger exit notification handler
- Logs error with context (IP, operation type) and continues operation
- Prevents single goroutine panic from crashing entire service

**6. Non-Blocking Operations:**
- SNMP scans for newly discovered devices run in background goroutines to avoid blocking discovery loop
- Pinger exit notifications use buffered channel (`pingerExitChan`, capacity 100) to prevent blocking pinger shutdown
- Rate limiter uses `golang.org/x/time/rate` package for non-blocking ping rate control

### Graceful Shutdown Sequence

When shutdown signal (SIGINT or SIGTERM) is received:

1. Signal received on `sigChan` in shutdown handler goroutine
2. Shutdown handler calls `stop()` function, canceling main context (`mainCtx`)
3. Main event loop receives `<-mainCtx.Done()` in select case, enters shutdown block
4. Stop all tickers explicitly via `.Stop()` calls
5. Acquire `pingersMu` lock for exclusive access
6. Iterate `activePingers` map and call all cancel functions: `for ip, cancel := range activePingers { cancel() }`
7. Release `pingersMu` lock
8. Call `pingerWg.Wait()` to block until all pinger goroutines exit (each pinger checks `ctx.Done()` and exits gracefully)
9. Call `writer.Close()` to flush remaining batched points:
   - Cancels batch flusher context
   - Drains points from batch channel
   - Flushes remaining points to both WriteAPIs (primary and health buckets)
   - Closes InfluxDB client
10. Log "Shutdown complete" and return from `main()`

**Guarantees:**
- All active monitoring stops cleanly
- No data loss - all queued metrics flushed to InfluxDB
- No goroutine leaks
- Clean process exit

---

## 2. Development Setup

### Prerequisites

- **Go 1.25 or later** - Required for build
- **Git** - For cloning repository
- **Docker & Docker Compose** - For local InfluxDB (recommended for testing)
- **make** (optional) - For convenience commands

### Clone Repository

```bash
git clone https://github.com/kljama/netscan.git
cd netscan
```

### Download Dependencies

```bash
go mod download
```

This downloads all dependencies specified in `go.mod`:
- `gopkg.in/yaml.v3` - YAML parsing
- `github.com/gosnmp/gosnmp` - SNMPv2c protocol
- `github.com/prometheus-community/pro-bing` - ICMP ping
- `github.com/influxdata/influxdb-client-go/v2` - InfluxDB client
- `github.com/rs/zerolog` - Structured logging
- `golang.org/x/time` - Rate limiting

### Set Up Local InfluxDB for Testing

**Option 1: Using Docker Compose (Recommended)**

Start only InfluxDB from the project's docker-compose.yml:

```bash
docker compose up -d influxdb
```

This creates:
- InfluxDB container on port 8086
- Organization: `test-org`
- Admin token: `netscan-token`
- Primary bucket: `netscan`
- Health bucket: `health`

Access InfluxDB UI at http://localhost:8086
- Username: `admin`
- Password: `admin123`

**Option 2: Manual Docker Run**

```bash
docker run -d \
  --name influxdb \
  -p 8086:8086 \
  -e DOCKER_INFLUXDB_INIT_MODE=setup \
  -e DOCKER_INFLUXDB_INIT_USERNAME=admin \
  -e DOCKER_INFLUXDB_INIT_PASSWORD=admin123 \
  -e DOCKER_INFLUXDB_INIT_ORG=test-org \
  -e DOCKER_INFLUXDB_INIT_BUCKET=netscan \
  -e DOCKER_INFLUXDB_INIT_ADMIN_TOKEN=netscan-token \
  influxdb:2.7
```

**Create health bucket manually:**

```bash
# Via CLI
docker exec influxdb influx bucket create \
  -n health \
  -o test-org \
  -t netscan-token

# Or via UI at http://localhost:8086
```

### Configure for Development

```bash
# Copy config template
cp config.yml.example config.yml

# Edit with your test network
nano config.yml
```

**Minimal development config:**

```yaml
networks:
  - "127.0.0.1/32"  # Localhost only for testing

icmp_discovery_interval: "1m"
ping_interval: "5s"
ping_timeout: "3s"

snmp:
  community: "public"
  port: 161
  timeout: "5s"
  retries: 1

influxdb:
  url: "http://localhost:8086"
  token: "netscan-token"
  org: "test-org"
  bucket: "netscan"
  health_bucket: "health"
  batch_size: 100  # Smaller for dev
  flush_interval: "2s"  # Faster for dev

health_check_port: 8080
health_report_interval: "5s"
```

**For localhost testing:** The 127.0.0.1/32 network will only ping localhost, which will always respond. This is useful for testing the monitoring loop without requiring actual network devices.

### Verify Setup

```bash
# Check InfluxDB is running
curl http://localhost:8086/health

# Expected output: {"name":"influxdb","message":"ready for queries and writes","status":"pass",...}
```

---

## 3. Building & Testing

### Build Binary

```bash
# Standard build
go build -o netscan ./cmd/netscan

# Build with version info (recommended)
go build -ldflags="-w -s" -o netscan ./cmd/netscan

# Run the binary
sudo ./netscan -config config.yml
# Note: sudo required for CAP_NET_RAW (ICMP raw sockets)
```

**Grant capability to avoid sudo:**

```bash
# Set capability on binary
sudo setcap cap_net_raw+ep ./netscan

# Now run without sudo
./netscan -config config.yml
```

### Run Tests

**Run all tests:**

```bash
go test ./...
```

**Run tests with verbose output:**

```bash
go test -v ./...
```

**Run tests with race detection (CRITICAL):**

```bash
go test -race ./...
```

Race detection is **mandatory** before committing any changes. It detects data races in concurrent code.

**Run tests for specific package:**

```bash
go test ./internal/state
go test ./internal/influx
go test ./internal/monitoring
```

**Run specific test:**

```bash
go test -v ./internal/state -run TestAddDevice
```

**Run with coverage:**

```bash
go test -cover ./...
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out  # View in browser
```

### Test Organization

Tests are co-located with source files using the `_test.go` suffix:

```
internal/
├── state/
│   ├── manager.go
│   ├── manager_test.go
│   ├── manager_concurrent_test.go
│   └── manager_circuitbreaker_test.go
├── influx/
│   ├── writer.go
│   ├── writer_test.go
│   └── writer_validation_test.go
├── monitoring/
│   ├── pinger.go
│   ├── pinger_test.go
│   ├── pinger_ratelimit_test.go
│   └── pinger_success_test.go
```

**Test Categories:**

- **Unit Tests** (`*_test.go`) - Test individual functions/methods
- **Concurrent Tests** (`*_concurrent_test.go`) - Test thread-safety with goroutines
- **Integration Tests** (`*_integration_test.go`) - Test interactions between components
- **Circuit Breaker Tests** (`*_circuitbreaker_test.go`) - Test failure handling logic

### Linting

**Using golangci-lint (recommended):**

```bash
# Install
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Run
golangci-lint run
```

**Built-in tools:**

```bash
# Format code
go fmt ./...

# Check for suspicious constructs
go vet ./...
```

### Continuous Integration

The project uses GitHub Actions for CI/CD. See `.github/workflows/ci-cd.yml`:

**CI Workflow includes:**
1. Go version matrix testing (1.25.x)
2. Dependency download
3. `go fmt` check
4. `go vet` check
5. `go test -race ./...`
6. `go build`
7. `govulncheck` security scanning
8. Docker image build test

**Before pushing commits, ensure:**
```bash
go fmt ./...
go vet ./...
go test -race ./...
go build ./cmd/netscan
```

---

## 4. Contribution Guidelines

### Coding Standards

Based on `copilot-instructions.md` guiding principles and mandates:

#### 1. Decoupled & Concurrent Design

- New services MUST be implemented as decoupled, concurrent goroutines
- Orchestrate with dedicated Ticker in `main.go`
- Must not block other services
- Example: Adding new monitoring workflow = new ticker in main event loop

#### 2. Centralized State (StateManager)

- StateManager is the **single source of truth** for device state
- **Never** create separate device lists
- All device interactions go through StateManager's thread-safe methods
- Example: `stateMgr.AddDevice()`, `stateMgr.UpdateLastSeen()`, etc.

#### 3. Resilience First

All code interacting with external services (networks, databases, APIs) MUST implement:

a. **Aggressive `context.WithTimeout`**
```go
ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
defer cancel()
```

b. **Robust error handling** - Log and continue, never `panic`
```go
if err != nil {
    log.Error().Err(err).Str("ip", ip).Msg("Operation failed")
    return  // or continue
}
```

c. **Client-side rate limiting** where appropriate
```go
if err := limiter.Wait(ctx); err != nil {
    return
}
```

#### 4. Configurable & Backward-Compatible

- All new parameters MUST be added to `config.yml`
- Support environment variable overrides using `os.ExpandEnv()`
- Include sensible defaults
- Existing `config.yml` files must continue working

```go
// config.go
type Config struct {
    NewFeature time.Duration `yaml:"new_feature_interval"`
}

// LoadConfig() - apply defaults
if raw.NewFeature == "" {
    cfg.NewFeature = 5 * time.Minute  // sensible default
}
```

#### 5. Testability

- New features must be testable
- Use interfaces for dependencies (like `PingWriter`, `StateManager`)
- Enable easy mocking in unit tests

```go
// Good - testable with interface
func ProcessDevice(device Device, writer PingWriter) error {
    return writer.WritePingResult(device.IP, rtt, true)
}

// In test: mock writer implements PingWriter interface
```

#### 6. Secure by Default

- All string data from external sources (SNMP, device responses) MUST be sanitized
- Use validation functions before writing to InfluxDB or logging

```go
// Always use helpers for SNMP data
hostname, err := validateSNMPString(value, "sysName")
sanitized := sanitizeInfluxString(hostname, "hostname")
```

### SNMP Compatibility Mandates

**Critical:** All new SNMP queries MUST use `snmpGetWithFallback()`:

```go
// REQUIRED pattern
resp, err := snmpGetWithFallback(params, oids)

// NOT ALLOWED
resp, err := params.Get(oids)  // Fails on some devices
```

**Why:** Some devices don't support `.0` instance OIDs. `snmpGetWithFallback()` tries `Get` first, falls back to `GetNext` if needed.

**All SNMP string processing MUST handle `[]byte` (OctetString):**

```go
func validateSNMPString(value interface{}, oidName string) (string, error) {
    switch v := value.(type) {
    case string:
        return v, nil
    case []byte:
        return string(v), nil  // Convert OctetString
    default:
        return "", fmt.Errorf("invalid type for %s", oidName)
    }
}
```

### Logging Standards

**Diagnostic logging requirements:**

1. **Log configuration values:**
```go
log.Info().Strs("networks", cfg.Networks).Msg("Scanning networks")
```

2. **Log entry/exit of major operations:**
```go
log.Info().Msg("Starting ICMP discovery scan...")
// ... operation ...
log.Info().Int("devices_found", len(ips)).Msg("ICMP discovery completed")
```

3. **Log errors with context:**
```go
log.Error().
    Str("ip", device.IP).
    Err(err).
    Msg("Failed to write device info to InfluxDB")
```

4. **Summary logs with counts:**
```go
log.Info().
    Int("enriched", len(snmpDevices)).
    Int("failed", len(allIPs)-len(snmpDevices)).
    Msg("SNMP scan complete")
```

### Testing Requirements

**Mandatory before commit:**

```bash
go test -race ./...
```

**Test coverage expectations:**
- New functions: Add unit tests
- State modifications: Add concurrent safety tests
- External interactions: Add integration tests
- Error paths: Test failure handling

**Example test structure:**

```go
func TestStateManager_AddDevice(t *testing.T) {
    mgr := NewManager(100)
    
    // Test new device
    isNew := mgr.AddDevice("192.168.1.1")
    if !isNew {
        t.Error("Expected new device")
    }
    
    // Test existing device
    isNew = mgr.AddDevice("192.168.1.1")
    if isNew {
        t.Error("Expected existing device")
    }
}
```

### Commit Message Format

Use **Conventional Commits** format:

```
<type>(<scope>): <subject>

<body>

<footer>
```

**Types:**
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation only
- `style`: Code formatting (no logic change)
- `refactor`: Code restructuring (no behavior change)
- `test`: Adding/updating tests
- `chore`: Maintenance tasks

**Examples:**

```
feat(discovery): add IPv6 support for ICMP sweeps

Implement IPv6 CIDR expansion and dual-stack ping workers.
Workers now handle both IPv4 and IPv6 addresses.

Closes #123
```

```
fix(state): prevent race condition in pinger reconciliation

Add stoppingPingers map to prevent starting new pinger
before old one exits. Fixes race when device quickly
re-discovered after pruning.

Fixes #456
```

```
docs(manual): update configuration reference with new parameters

Add documentation for ping_max_consecutive_fails and
ping_backoff_duration circuit breaker settings.
```

### Pull Request Requirements

**Before opening PR:**

1. **Code Quality:**
   - [ ] All tests pass: `go test ./...`
   - [ ] Race detection clean: `go test -race ./...`
   - [ ] Code formatted: `go fmt ./...`
   - [ ] Linting clean: `go vet ./...`
   - [ ] Build succeeds: `go build ./cmd/netscan`

2. **Documentation:**
   - [ ] `config.yml.example` updated with new parameters
   - [ ] `MANUAL.md` updated (if user-facing changes)
   - [ ] Code comments added for complex logic
   - [ ] Commit messages follow Conventional Commits

3. **Testing:**
   - [ ] Unit tests for new functions
   - [ ] Integration tests for component interactions
   - [ ] Concurrent tests for thread-safety (if applicable)
   - [ ] Circuit breaker tests for failure handling (if applicable)

4. **Security:**
   - [ ] No credentials in code
   - [ ] Input validation for external data
   - [ ] SNMP strings sanitized
   - [ ] `govulncheck` passes

**PR Description Template:**

```markdown
## Summary
Brief description of changes

## Changes
- Bullet list of specific changes

## Testing
- How was this tested?
- Test coverage added/updated

## Documentation
- What documentation was updated?

## Breaking Changes
- Any backward-incompatible changes?

## Related Issues
Closes #<issue_number>
```

### Code Review Checklist

Reviewers should verify:

- [ ] Follows architectural boundaries (no UI, no new databases, read-only)
- [ ] Uses StateManager as single source of truth
- [ ] SNMP queries use `snmpGetWithFallback()`
- [ ] External data sanitized before storage
- [ ] Comprehensive error handling (no panics)
- [ ] Thread-safe concurrent access (mutexes/atomics)
- [ ] Tests include race detection
- [ ] Documentation updated (config.yml.example, MANUAL.md)
- [ ] Conventional commit messages
- [ ] No credentials in code

---

**End of Part II: Development Guide**

*Part III (Reference Documentation) will be added in the next update.*
