# netscan

netscan is a robust, long-running network monitoring service written in Go. It performs periodic SNMP discovery on large network ranges, continuously pings discovered devices, and writes performance metrics to InfluxDB.

## Features
- Periodic SNMPv2c discovery of large network ranges
- Continuous ICMP ping monitoring for active devices
- Metrics written to InfluxDB (RTT, success/failure)
- Thread-safe state management for device tracking
- Graceful shutdown and automatic device pruning
- Single binary deployment


## Quick Start
1. Copy `config.yml.example` to `config.yml` and edit with your network, SNMP, and InfluxDB settings.
2. Build the executable using the provided script:
  ```fish
  ./build.sh
  ```
  Or manually:
  ```fish
  go build -o netscan ./cmd/netscan
  ```

3. Run the service:
  ```fish
  ./netscan
  ```

## Usage Examples

### Basic Run
Start netscan with default config in the current directory:
```fish
./netscan
```

### Custom Config Location
Run netscan with a custom config file:
```fish
cp config.yml.example /opt/netscan/config.yml
cd /opt/netscan
./netscan
```

### Systemd Service Management
Check status:
```fish
sudo systemctl status netscan
```
Restart service:
```fish
sudo systemctl restart netscan
```
View logs:
```fish
sudo journalctl -u netscan -f
```

### Testing
Run all unit tests:
```fish
go test ./...
```

## Troubleshooting
- Ensure InfluxDB is reachable from the host.
- Check SNMP and ICMP permissions/firewall rules.
- Review logs for errors and device status changes.

## Contributing
Pull requests and issues are welcome! Please add tests for new features and follow Go best practices.

## Deployment
To install and run netscan as a systemd service, use the provided deployment script:
```fish
sudo ./deploy.sh
```
This will build (if needed), install, and start netscan as a service. See the script for details and customization.

## Configuration
All settings are managed in `config.yml`. Example:

```yaml
# Scan settings
discovery_interval: "4h"
networks:
  - "10.20.0.0/18"

# SNMP settings (v2c)
snmp:
  community: "public"
  port: 161
  timeout: "5s"
  retries: 1

# ICMP Ping settings
ping_interval: "2s"
ping_timeout: "2s"

# InfluxDB settings
influxdb:
  url: "http://localhost:8086"
  token: "YOUR_INFLUXDB_API_TOKEN"
  org: "your-org"
  bucket: "netscan"
```

## Architecture
- `cmd/netscan/main.go`: Main entry point, orchestrates all modules
- `internal/config/`: Loads and parses configuration
- `internal/state/`: Thread-safe device state manager
- `internal/influx/`: InfluxDB writer for ping results
- `internal/monitoring/`: ICMP pinger logic
- `internal/discovery/`: SNMP scanner for device discovery

## Testing
Run all unit tests:
```fish
go test ./...
```
Tests cover configuration parsing, state management, SNMP IP generation, InfluxDB writing, and pinger logic.

## Deployment
Build a single binary and deploy to your target system. Ensure InfluxDB is accessible and SNMP/ICMP traffic is permitted.

## License
MIT
