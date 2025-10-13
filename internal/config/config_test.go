package config

import (
	"os"
	"testing"
)

func TestLoadConfigValid(t *testing.T) {
	f, err := os.CreateTemp("", "config_test_*.yml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	configYAML := `discovery_interval: "1h"
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
