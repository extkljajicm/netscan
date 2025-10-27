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
