package config

import (
	"os"
	"testing"
	"time"
)

// TestValidateConfigTimeoutWarning tests that the config validation warns
// when ping_timeout <= ping_interval (dangerous configuration).
func TestValidateConfigTimeoutWarning(t *testing.T) {
	tests := []struct {
		name          string
		pingInterval  time.Duration
		pingTimeout   time.Duration
		expectWarning bool
		warningText   string
	}{
		{
			name:          "Timeout less than interval - should warn",
			pingInterval:  3 * time.Second,
			pingTimeout:   2 * time.Second,
			expectWarning: true,
			warningText:   "WARNING: ping_timeout should be greater than ping_interval",
		},
		{
			name:          "Timeout equal to interval - should warn",
			pingInterval:  2 * time.Second,
			pingTimeout:   2 * time.Second,
			expectWarning: true,
			warningText:   "WARNING: ping_timeout should be greater than ping_interval",
		},
		{
			name:          "Timeout greater than interval - no warning",
			pingInterval:  2 * time.Second,
			pingTimeout:   3 * time.Second,
			expectWarning: false,
			warningText:   "",
		},
		{
			name:          "Recommended: interval + 1s - no warning",
			pingInterval:  2 * time.Second,
			pingTimeout:   3 * time.Second,
			expectWarning: false,
			warningText:   "",
		},
		{
			name:          "Large margin - no warning",
			pingInterval:  2 * time.Second,
			pingTimeout:   10 * time.Second,
			expectWarning: false,
			warningText:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary config file
			f, err := os.CreateTemp("", "test-config-*.yml")
			if err != nil {
				t.Fatal(err)
			}
			defer os.Remove(f.Name())

			// Write minimal valid config
			configYAML := `
networks:
  - "192.168.1.0/24"
icmp_discovery_interval: "5m"
ping_interval: "` + tt.pingInterval.String() + `"
ping_timeout: "` + tt.pingTimeout.String() + `"
snmp:
  community: "test-community-123"
  port: 161
influxdb:
  url: "http://localhost:8086"
  token: "test-token"
  org: "test-org"
  bucket: "test-bucket"
`
			if _, err := f.WriteString(configYAML); err != nil {
				t.Fatal(err)
			}
			f.Close()

			// Load config
			cfg, err := LoadConfig(f.Name())
			if err != nil {
				t.Fatalf("failed to load config: %v", err)
			}

			// Validate config
			warning, err := ValidateConfig(cfg)
			if err != nil {
				t.Fatalf("validation failed with error: %v", err)
			}

			// Check warning
			if tt.expectWarning {
				if warning == "" {
					t.Errorf("expected warning but got none")
				}
				if warning != "" && tt.warningText != "" {
					// Check that warning contains expected text
					if len(warning) < len(tt.warningText) || warning[:len(tt.warningText)] != tt.warningText {
						t.Errorf("expected warning to start with %q, got %q", tt.warningText, warning)
					}
				}
			} else {
				if warning != "" {
					t.Errorf("expected no warning but got: %s", warning)
				}
			}
		})
	}
}

// TestDefaultWorkerCounts validates the new safer defaults for worker counts.
func TestDefaultWorkerCounts(t *testing.T) {
	f, err := os.CreateTemp("", "test-config-*.yml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	// Config without worker counts specified - should use defaults
	configYAML := `
networks:
  - "192.168.1.0/24"
icmp_discovery_interval: "5m"
ping_interval: "2s"
ping_timeout: "3s"
snmp:
  community: "test-community-123"
  port: 161
influxdb:
  url: "http://localhost:8086"
  token: "test-token"
  org: "test-org"
  bucket: "test-bucket"
`
	if _, err := f.WriteString(configYAML); err != nil {
		t.Fatal(err)
	}
	f.Close()

	cfg, err := LoadConfig(f.Name())
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	// Verify new safer defaults
	if cfg.IcmpWorkers != 64 {
		t.Errorf("expected default IcmpWorkers=64, got %d", cfg.IcmpWorkers)
	}

	if cfg.SnmpWorkers != 32 {
		t.Errorf("expected default SnmpWorkers=32, got %d", cfg.SnmpWorkers)
	}
}
