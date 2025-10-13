package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type SNMPConfig struct {
	Community string        `yaml:"community"`
	Port      int           `yaml:"port"`
	Timeout   time.Duration `yaml:"timeout"`
	Retries   int           `yaml:"retries"`
}

type InfluxDBConfig struct {
	URL    string `yaml:"url"`
	Token  string `yaml:"token"`
	Org    string `yaml:"org"`
	Bucket string `yaml:"bucket"`
}

type Config struct {
	DiscoveryInterval time.Duration  `yaml:"discovery_interval"`
	Networks          []string       `yaml:"networks"`
	SNMP              SNMPConfig     `yaml:"snmp"`
	PingInterval      time.Duration  `yaml:"ping_interval"`
	PingTimeout       time.Duration  `yaml:"ping_timeout"`
	InfluxDB          InfluxDBConfig `yaml:"influxdb"`
}

func LoadConfig(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var raw struct {
		DiscoveryInterval string         `yaml:"discovery_interval"`
		Networks          []string       `yaml:"networks"`
		SNMP              SNMPConfig     `yaml:"snmp"`
		PingInterval      string         `yaml:"ping_interval"`
		PingTimeout       string         `yaml:"ping_timeout"`
		InfluxDB          InfluxDBConfig `yaml:"influxdb"`
	}

	decoder := yaml.NewDecoder(f)
	if err := decoder.Decode(&raw); err != nil {
		return nil, err
	}

	discoveryInterval, err := time.ParseDuration(raw.DiscoveryInterval)
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
	if raw.SNMP.Timeout == 0 {
		raw.SNMP.Timeout = 5 * time.Second
	}

	return &Config{
		DiscoveryInterval: discoveryInterval,
		Networks:          raw.Networks,
		SNMP:              raw.SNMP,
		PingInterval:      pingInterval,
		PingTimeout:       pingTimeout,
		InfluxDB:          raw.InfluxDB,
	}, nil
}
