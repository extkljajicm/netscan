# netscan - Native Installation Guide

This guide covers native installation and deployment of netscan on Linux systems. For Docker deployment (recommended), see the main [README.md](README.md).

## Prerequisites

- Go 1.21+ (tested with 1.25.1)
- InfluxDB 2.x
- Root privileges for ICMP socket access (or CAP_NET_RAW capability)

## Installation

### Ubuntu

```bash
sudo apt update
sudo apt install golang-go
```

### CachyOS

```bash
sudo pacman -S go
```

### Setup

```bash
git clone https://github.com/extkljajicm/netscan.git
cd netscan
go mod download
```

## Building

### Automated Build

```bash
./build.sh
```

Builds the netscan binary with optimized settings.

### Manual Build

```bash
go build -o netscan ./cmd/netscan
```

### Cross-Platform Builds

For custom platform builds, use Go's cross-compilation:
```bash
GOOS=linux GOARCH=arm64 go build -o netscan-arm64 ./cmd/netscan
GOOS=darwin GOARCH=amd64 go build -o netscan-macos ./cmd/netscan
```

## Configuration

### Create Configuration File

```bash
cp config.yml.example config.yml
```

Edit `config.yml` with your network settings.

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
INFLUXDB_TOKEN=your-secure-token        # InfluxDB API token
INFLUXDB_ORG=your-org                   # InfluxDB organization
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

## Deployment

### Automated Deployment (Recommended)

```bash
sudo ./deploy.sh
```

Creates:
- `/opt/netscan/` with binary, config, and secure `.env` file
- `netscan` user with minimal privileges
- `CAP_NET_RAW` capability on binary
- Systemd service with network-compatible security settings
- Secure credential management via environment variables

### Manual Deployment

```bash
# Build the binary
go build -o netscan ./cmd/netscan

# Create installation directory
sudo mkdir -p /opt/netscan

# Copy files
sudo cp netscan /opt/netscan/
sudo cp config.yml /opt/netscan/

# Set capability for ICMP
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

# Load environment variables from .env file
EnvironmentFile=/opt/netscan/.env

# Security settings (relaxed for network access)
NoNewPrivileges=yes
PrivateTmp=yes
ProtectSystem=strict
AmbientCapabilities=CAP_NET_RAW

[Install]
WantedBy=multi-user.target
EOF

# Enable and start service
sudo systemctl daemon-reload
sudo systemctl enable netscan
sudo systemctl start netscan
```

## Service Management

### Systemd Commands

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

# Disable service
sudo systemctl disable netscan
```

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

Set up InfluxDB and test with your configuration:

```bash
# Manually start InfluxDB (or use Docker)
# Edit config.yml with InfluxDB credentials
./netscan -config config.yml
```

## Troubleshooting

### ICMP Permission Denied

```bash
# Check capability
getcap /opt/netscan/netscan

# Manually set capability
sudo setcap cap_net_raw+ep /opt/netscan/netscan

# Or run with sudo (not recommended)
sudo ./netscan
```

### InfluxDB Connection Issues

- Verify InfluxDB is running: `systemctl status influxdb`
- Check config credentials and API token
- Confirm network connectivity: `curl http://localhost:8086/health`
- Review logs: `journalctl -u netscan -f`

### No Devices Discovered

- Verify network ranges in config.yml (e.g., `192.168.1.0/24`)
- Check firewall rules for ICMP/SNMP
- Confirm SNMP community string is correct
- Test with ping manually: `ping <device-ip>`
- Check logs for specific errors

### Performance Issues

- Start with lower worker counts (8 ICMP, 4 SNMP)
- Monitor CPU usage with `htop` or `top`
- Adjust based on network latency and CPU cores
- Check memory usage: `journalctl -u netscan | grep memory`

### Service Won't Start

```bash
# Check service status
sudo systemctl status netscan

# View detailed logs
sudo journalctl -u netscan -n 50

# Verify configuration
./netscan -config /opt/netscan/config.yml

# Check file permissions
ls -la /opt/netscan/
```

## Uninstallation

### Automated Cleanup

```bash
sudo ./undeploy.sh
```

Removes:
- Systemd service
- Service user
- Installation directory
- All netscan files

### Manual Cleanup

```bash
# Stop and disable service
sudo systemctl stop netscan
sudo systemctl disable netscan

# Remove service file
sudo rm /etc/systemd/system/netscan.service
sudo systemctl daemon-reload

# Remove installation
sudo rm -rf /opt/netscan

# Remove user
sudo userdel netscan
```

## Performance Tuning

For large networks, adjust these settings in `config.yml`:

```yaml
# Increase for better performance
icmp_workers: 128              # More concurrent ICMP operations
snmp_workers: 64               # More concurrent SNMP operations

# Decrease for resource constraints
max_concurrent_pingers: 500    # Limit active monitoring
max_devices: 5000              # Limit total devices

# Adjust intervals
icmp_discovery_interval: "10m" # Less frequent discovery
ping_interval: "5s"            # Less frequent pings
```

**Monitoring Resource Usage:**
```bash
# CPU and memory
htop

# Disk I/O
iostat -x 1

# Network connections
ss -s
```

## Development

For development and testing:

```bash
# Run without installation
go run ./cmd/netscan -config config.yml

# Build with race detection
go build -race -o netscan ./cmd/netscan

# Run tests with coverage
go test -cover ./...

# Format code
go fmt ./...

# Run linter (if available)
golangci-lint run
```

## Additional Resources

- Main README: [README.md](README.md)
- Docker Setup: See main README for Docker Compose deployment
- Configuration Reference: See `config.yml.example`
- Architecture Details: See main README
