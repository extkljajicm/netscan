# netscan

A robust, long-running network monitoring service in Go.

## Features
- Periodic SNMP discovery of large network ranges
- Continuous ICMP ping monitoring
- Metrics written to InfluxDB
- Configurable via `config.yml`

## Usage
1. Copy `config.yml.example` to `config.yml` and edit as needed.
2. Build the executable:
   ```fish
   go build -o netscan ./cmd/netscan
   ```
3. Run the service:
   ```fish
   ./netscan
   ```

## Configuration
See `config.yml.example` for all available options.

## Project Structure
- `cmd/netscan/main.go`: Main entry point
- `internal/config/`: Config loader
- `internal/state/`: Device state manager
- `internal/influx/`: InfluxDB writer
- `internal/monitoring/`: ICMP pinger
- `internal/discovery/`: SNMP scanner

## License
MIT
