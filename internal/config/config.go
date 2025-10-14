package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// SNMPConfig holds SNMPv2c connection parameters
type SNMPConfig struct {
	Community string        `yaml:"community"`
	Port      int           `yaml:"port"`
	Timeout   time.Duration `yaml:"timeout"`
	Retries   int           `yaml:"retries"`
}

// InfluxDBConfig holds InfluxDB v2 connection parameters
type InfluxDBConfig struct {
	URL    string `yaml:"url"`
	Token  string `yaml:"token"`
	Org    string `yaml:"org"`
	Bucket string `yaml:"bucket"`
}

// Config holds all application configuration parameters
type Config struct {
	DiscoveryInterval     time.Duration  `yaml:"discovery_interval"`
	IcmpDiscoveryInterval time.Duration  `yaml:"icmp_discovery_interval"`
	IcmpWorkers           int            `yaml:"icmp_workers"`
	SnmpWorkers           int            `yaml:"snmp_workers"`
	Networks              []string       `yaml:"networks"`
	SNMP                  SNMPConfig     `yaml:"snmp"`
	PingInterval          time.Duration  `yaml:"ping_interval"`
	PingTimeout           time.Duration  `yaml:"ping_timeout"`
	InfluxDB              InfluxDBConfig `yaml:"influxdb"`
}

// LoadConfig parses YAML configuration file and returns Config struct
func LoadConfig(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Raw config struct for YAML parsing with string duration fields
	var raw struct {
		DiscoveryInterval     string         `yaml:"discovery_interval"`
		IcmpDiscoveryInterval string         `yaml:"icmp_discovery_interval"`
		IcmpWorkers           int            `yaml:"icmp_workers"`
		SnmpWorkers           int            `yaml:"snmp_workers"`
		Networks              []string       `yaml:"networks"`
		SNMP                  SNMPConfig     `yaml:"snmp"`
		PingInterval          string         `yaml:"ping_interval"`
		PingTimeout           string         `yaml:"ping_timeout"`
		InfluxDB              InfluxDBConfig `yaml:"influxdb"`
	}

	decoder := yaml.NewDecoder(f)
	if err := decoder.Decode(&raw); err != nil {
		return nil, err
	}

	// Parse string durations to time.Duration
	discoveryInterval, err := time.ParseDuration(raw.DiscoveryInterval)
	if err != nil {
		return nil, err
	}
	icmpDiscoveryInterval, err := time.ParseDuration(raw.IcmpDiscoveryInterval)
	if err != nil {
		return nil, err
	}
	pingInterval, err := time.ParseDuration(raw.PingInterval)
	if err != nil {
		return nil, err
	}
	pingTimeout, err := time.ParseDuration(raw.PingTimeout)
	if err != nil {
		return nil, err
	}

	// Set default SNMP timeout if not specified
	if raw.SNMP.Timeout == 0 {
		raw.SNMP.Timeout = 5 * time.Second
	}

	// Set default values if not specified
	if raw.IcmpWorkers == 0 {
		raw.IcmpWorkers = 64
	}
	if raw.SnmpWorkers == 0 {
		raw.SnmpWorkers = 32
	}

	return &Config{
		DiscoveryInterval:     discoveryInterval,
		IcmpDiscoveryInterval: icmpDiscoveryInterval,
		IcmpWorkers:           raw.IcmpWorkers,
		SnmpWorkers:           raw.SnmpWorkers,
		Networks:              raw.Networks,
		SNMP:                  raw.SNMP,
		PingInterval:          pingInterval,
		PingTimeout:           pingTimeout,
		InfluxDB:              raw.InfluxDB,
	}, nil
}
