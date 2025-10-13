package discovery

import (
	"net"
	"testing"
)

func TestIpsFromCIDR(t *testing.T) {
	ips := ipsFromCIDR("192.168.1.0/30")
	want := []string{"192.168.1.0", "192.168.1.1", "192.168.1.2", "192.168.1.3"}
	if len(ips) != len(want) {
		t.Fatalf("expected %d IPs, got %d", len(want), len(ips))
	}
	for i, ip := range want {
		if ips[i] != ip {
			t.Errorf("expected %s, got %s", ip, ips[i])
		}
	}
}

func TestIncIP(t *testing.T) {
	ip := net.ParseIP("192.168.1.1")
	incIP(ip)
	if ip.String() != "192.168.1.2" {
		t.Errorf("expected 192.168.1.2, got %s", ip.String())
	}
}
