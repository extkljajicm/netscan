package config

import (
	"os"
	"testing"
	"time"
)

func TestLoadConfigValid(t *testing.T) {
	f, err := os.CreateTemp("", "config_test_*.yml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	configYAML := `discovery_interval: "1h"
icmp_discovery_interval: "5m"
icmp_workers: 64
snmp_workers: 32
networks:
  - "192.168.1.0/30"
snmp:
  community: "public"
  port: 161
  timeout: "5s"
  retries: 1
ping_interval: "10s"
ping_timeout: "1s"
influxdb:
  url: "http://localhost:8086"
  token: "token"
  org: "org"
  bucket: "bucket"
`
	if _, err := f.WriteString(configYAML); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(f.Name())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.DiscoveryInterval.String() != "1h0m0s" {
		t.Errorf("expected 1h, got %v", cfg.DiscoveryInterval)
	}
	if len(cfg.Networks) != 1 || cfg.Networks[0] != "192.168.1.0/30" {
		t.Errorf("networks not parsed correctly")
	}
}

func TestLoadConfigInvalid(t *testing.T) {
	f, err := os.CreateTemp("", "config_test_invalid_*.yml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString("not: valid: yaml"); err != nil {
		t.Fatal(err)
	}
	_, err = LoadConfig(f.Name())
	if err == nil {
		t.Errorf("expected error for invalid yaml")
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	f, err := os.CreateTemp("", "config_test_defaults_*.yml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	configYAML := `icmp_discovery_interval: "5m"
networks:
  - "192.168.1.0/30"
snmp:
  community: "testcommunity"
  port: 161
ping_interval: "10s"
ping_timeout: "1s"
influxdb:
  url: "http://localhost:8086"
  token: "token"
  org: "org"
  bucket: "bucket"
`
	if _, err := f.WriteString(configYAML); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(f.Name())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	
	// Test health_bucket default
	if cfg.InfluxDB.HealthBucket != "health" {
		t.Errorf("expected health_bucket default to be 'health', got %s", cfg.InfluxDB.HealthBucket)
	}
	
	// Test health_report_interval default
	if cfg.HealthReportInterval != 10*time.Second {
		t.Errorf("expected health_report_interval default to be 10s, got %v", cfg.HealthReportInterval)
	}
}
