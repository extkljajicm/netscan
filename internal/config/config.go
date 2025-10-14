package config

import (
	"fmt"
	"net"
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

	// Apply environment variable expansion to sensitive fields
	raw.InfluxDB.URL = expandEnv(raw.InfluxDB.URL)
	raw.InfluxDB.Token = expandEnv(raw.InfluxDB.Token)
	raw.InfluxDB.Org = expandEnv(raw.InfluxDB.Org)
	raw.InfluxDB.Bucket = expandEnv(raw.InfluxDB.Bucket)
	raw.SNMP.Community = expandEnv(raw.SNMP.Community)

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

// expandEnv expands environment variables in a string, supporting ${VAR} and $VAR syntax
func expandEnv(s string) string {
	return os.ExpandEnv(s)
}

// ValidateConfig performs security and sanity checks on the configuration
func ValidateConfig(cfg *Config) error {
	// Validate network ranges
	for _, network := range cfg.Networks {
		if err := validateCIDR(network); err != nil {
			return err
		}
	}

	// Validate worker counts
	if cfg.IcmpWorkers < 1 || cfg.IcmpWorkers > 1000 {
		return fmt.Errorf("icmp_workers must be between 1 and 1000, got %d", cfg.IcmpWorkers)
	}
	if cfg.SnmpWorkers < 1 || cfg.SnmpWorkers > 500 {
		return fmt.Errorf("snmp_workers must be between 1 and 500, got %d", cfg.SnmpWorkers)
	}

	// Validate intervals
	if cfg.DiscoveryInterval < time.Minute {
		return fmt.Errorf("discovery_interval must be at least 1 minute, got %v", cfg.DiscoveryInterval)
	}
	if cfg.IcmpDiscoveryInterval < time.Minute {
		return fmt.Errorf("icmp_discovery_interval must be at least 1 minute, got %v", cfg.IcmpDiscoveryInterval)
	}
	if cfg.PingInterval < time.Second {
		return fmt.Errorf("ping_interval must be at least 1 second, got %v", cfg.PingInterval)
	}

	// Validate SNMP settings
	if cfg.SNMP.Port < 1 || cfg.SNMP.Port > 65535 {
		return fmt.Errorf("snmp port must be between 1 and 65535, got %d", cfg.SNMP.Port)
	}
	if cfg.SNMP.Timeout < time.Second {
		return fmt.Errorf("snmp timeout must be at least 1 second, got %v", cfg.SNMP.Timeout)
	}
	if cfg.SNMP.Retries < 0 || cfg.SNMP.Retries > 10 {
		return fmt.Errorf("snmp retries must be between 0 and 10, got %d", cfg.SNMP.Retries)
	}

	// Validate required fields
	if cfg.InfluxDB.URL == "" {
		return fmt.Errorf("influxdb.url is required")
	}
	if cfg.InfluxDB.Token == "" {
		return fmt.Errorf("influxdb.token is required")
	}
	if cfg.InfluxDB.Org == "" {
		return fmt.Errorf("influxdb.org is required")
	}
	if cfg.InfluxDB.Bucket == "" {
		return fmt.Errorf("influxdb.bucket is required")
	}
	if cfg.SNMP.Community == "" {
		return fmt.Errorf("snmp.community is required")
	}

	return nil
}

// validateCIDR validates a CIDR notation and checks for dangerous network ranges
func validateCIDR(cidr string) error {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("invalid CIDR notation: %s", cidr)
	}

	// Check for dangerous network ranges
	networkIP := network.IP
	if networkIP.IsLoopback() {
		return fmt.Errorf("loopback networks not allowed: %s", cidr)
	}
	if networkIP.IsMulticast() {
		return fmt.Errorf("multicast networks not allowed: %s", cidr)
	}
	if networkIP.IsLinkLocalUnicast() {
		return fmt.Errorf("link-local networks not allowed: %s", cidr)
	}

	// Check for overly broad ranges (larger than /8)
	ones, _ := network.Mask.Size()
	if ones < 8 {
		return fmt.Errorf("network range too broad (/%d), maximum allowed is /8: %s", ones, cidr)
	}

	return nil
}
