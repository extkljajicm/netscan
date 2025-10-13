GitHub Copilot Instructions for Project: netscan
Project Goal
Create a robust, long-running network monitoring service in Go. The service will perform periodic SNMP discovery on large network ranges to find active devices. For each discovered device, it will initiate continuous ICMP pinging and write the performance metrics to an InfluxDB time-series database. The final output should be a single executable file for easy deployment.

Core Features
Configuration: Load all parameters (network ranges, intervals, SNMP credentials, InfluxDB details) from a single config.yml file.

Periodic Discovery: On a configurable interval (e.g., every 4 hours), scan the defined /18 subnet using a high-concurrency worker pool to find devices via SNMPv2c.

State Management: Maintain a thread-safe, in-memory list of currently active devices. The system must automatically start monitoring new devices and stop monitoring devices that are no longer found.

Continuous Monitoring: For every active device, run a dedicated goroutine that performs an ICMP ping on a separate, frequent interval (e.g., every 30 seconds).

Data Persistence: Write the results of every ICMP ping (Round-Trip Time, success/failure) to an InfluxDB bucket.

Technology Stack
Language: Go

Key Libraries:

gopkg.in/yaml.v3 (for config parsing)

github.com/gosnmp/gosnmp (for SNMPv2c)

github.com/go-ping/ping (for ICMP)

github.com/influxdata/influxdb-client-go/v2 (for InfluxDB)

Desired Project Structure
/netscan
├── cmd/netscan/
│   └── main.go         # Main application entry point and service orchestration.
├── internal/
│   ├── config/
│   │   └── config.go   # Logic for loading and parsing config.yml.
│   ├── discovery/
│   │   └── scanner.go  # The periodic, concurrent SNMP scanner.
│   ├── influx/
│   │   └── writer.go   # A simple wrapper for the InfluxDB client.
│   ├── monitoring/
│   │   └── pinger.go   # The continuous ICMP pinger logic.
│   └── state/
│       └── manager.go  # The thread-safe state manager for active devices.
├── go.mod
├── go.sum
└── config.yml.example
Step-by-Step Implementation Plan
Step 1: Initialize Project and config.yml.example
Initialize the Go module: go mod init github.com/marko/netscan.

Add the required dependencies using go get.

Create the config.yml.example file with the following structure:

YAML

# Scan settings
discovery_interval: "4h"  # How often to run the full SNMP discovery scan.
networks:
  - "10.20.0.0/18"

# SNMP settings (v2c)
snmp:
  community: "public"
  port: 161
  timeout: "5s"
  retries: 1

# ICMP Ping settings
ping_interval: "30s"
ping_timeout: "2s"

# InfluxDB settings
influxdb:
  url: "http://localhost:8086"
  token: "YOUR_INFLUXDB_API_TOKEN"
  org: "your-org"
  bucket: "netscan"
Step 2: Implement Configuration (internal/config/config.go)
Create a Go struct that mirrors the config.yml structure. Implement a LoadConfig(path string) (*Config, error) function that reads the file, parses it using yaml.v3, and returns the populated struct. Use time.ParseDuration for interval strings.

Step 3: Implement the State Manager (internal/state/manager.go)
This is the core of the service.

Define a Device struct: { IP string, Hostname string, SysDescr string, SysObjectID string }.

Create a Manager struct that contains a map like devices map[string]*Device and a sync.RWMutex to ensure thread safety.

Implement methods on the Manager:

Add(device Device): Adds a new device if it doesn't exist.

Get(ip string) (*Device, bool): Gets a device by IP.

GetAll() []Device: Returns a slice of all active devices.

UpdateLastSeen(ip string): A mechanism to track when a device was last found.

Prune(olderThan time.Duration): A method to remove devices that haven't been seen by the discovery scanner for a certain duration.

Step 4: Implement InfluxDB Writer (internal/influx/writer.go)
Create a Writer struct that holds the InfluxDB client API. Implement a single method: WritePingResult(ip, hostname string, rtt time.Duration, successful bool). This method should create a new Point and write it to InfluxDB.

Step 5: Implement the Pinger (internal/monitoring/pinger.go)
Create a function StartPinger(device state.Device, config *config.Config, writer *influx.Writer, ctx context.Context).
This function will be launched in its own goroutine for each device. It should contain a loop that:

Uses a time.NewTicker based on the ping_interval.

In each tick, it uses github.com/go-ping/ping to ping the device's IP.

Calls writer.WritePingResult() with the outcome.

Listens for the context to be cancelled to gracefully shut down the goroutine.

Step 6: Implement the Discovery Scanner (internal/discovery/scanner.go)
Create a RunScan(config *config.Config) function.

This function will implement the concurrent worker pool pattern. Create channels for jobs (IPs to scan) and results.

Launch a large number of worker goroutines.

A producer goroutine will generate all IPs from the CIDR ranges in the config and put them on the jobs channel.

Each worker will take an IP, perform an SNMP Get request for sysName, sysDescr, and sysObjectID.

If successful, it will package the data into a state.Device struct and send it to a results channel.

The main RunScan function will collect all results into a slice and return it.

Step 7: Orchestrate in main.go
The main function in cmd/netscan/main.go will tie everything together:

Load the configuration.

Initialize the state manager and the InfluxDB writer.

Create a map to keep track of running pinger goroutines, e.g., activePingers map[string]context.CancelFunc.

Start a main loop controlled by a time.NewTicker set to the discovery_interval.

On each tick:

Run the discovery scan to get a fresh list of found devices.

Iterate through the new list. For each device, check if a pinger is already running for it in your activePingers map. If not, launch a new pinger goroutine for it using go monitoring.StartPinger(...) and store its cancel function in the map.

Update a "last seen" timestamp for all devices found in the scan.

After the scan, call a prune function on the state manager to remove old devices whose pingers should be stopped (using the stored cancel function).

Implement graceful shutdown on receiving an interrupt signal (SIGINT).