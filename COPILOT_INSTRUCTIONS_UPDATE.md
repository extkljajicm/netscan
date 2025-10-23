# Recommended Updates to `.github/copilot-instructions.md`

Based on comprehensive analysis of the project, this document outlines specific additions and improvements to the GitHub Copilot instructions.

---

## Section to Add: Observability & Monitoring Mandates

**Insert after "Logging & Data Mandates" section:**

### Observability & Monitoring Mandates

* **Mandate: All services MUST expose health status.** Services must provide a way to verify they are running correctly. For network monitoring services like netscan, this includes:
    * Overall service health (running/degraded/down)
    * Dependency status (InfluxDB connectivity)
    * Current operational state (number of active devices, pingers, etc.)
* **Mandate: Critical metrics MUST be tracked.** Track key performance indicators:
    * Active device count
    * Active pinger count
    * Scan duration (ICMP discovery, SNMP scans)
    * InfluxDB write success/failure rates
    * Memory usage
    * Goroutine count
* **Mandate: Health checks MUST be Docker-compatible.** When adding health checks:
    * Implement HTTP endpoint (e.g., `/health`) on a configurable port
    * Return structured JSON with status details
    * Support both readiness and liveness concepts
    * Enable HEALTHCHECK directive in Dockerfile
    * Support Kubernetes probes format

---

## Section to Add: Testing Mandates

**Insert after "Testing & Validation Approach" section:**

### Testing Mandates (Additions)

* **Mandate: Orchestration logic MUST be tested.** The main ticker orchestration in `cmd/netscan/main.go` is critical and must have:
    * Integration tests for ticker lifecycle
    * Tests for graceful shutdown behavior
    * Tests for signal handling
    * Tests for pinger reconciliation logic
* **Mandate: Performance benchmarks MUST exist for hot paths.** Track performance over time:
    * Benchmark ICMP sweeps
    * Benchmark SNMP scans
    * Benchmark state manager operations under load
    * Benchmark InfluxDB write operations
* **Mandate: Resource limit enforcement MUST be tested.** Verify that:
    * `max_devices` limit is enforced
    * `max_concurrent_pingers` limit is enforced
    * Memory limits trigger warnings
    * Device eviction works correctly

---

## Section to Add: Security Mandates (Additions)

**Insert in "Core Principles & Mandates" after existing security content:**

### Security Scanning & Updates

* **Mandate: Dependencies MUST be scanned for vulnerabilities.** Use automated tools:
    * Run `govulncheck` in CI/CD pipeline
    * Scan Docker images with Trivy or similar
    * Address HIGH and CRITICAL CVEs within 30 days
    * Document accepted risks for LOW/MEDIUM issues
* **Mandate: Multi-architecture support SHOULD be considered.** Build for:
    * linux/amd64 (primary)
    * linux/arm64 (for ARM servers, newer Raspberry Pi)
    * linux/arm/v7 (for older Raspberry Pi)
* **Mandate: SNMPv3 support SHOULD be prioritized.** SNMPv2c uses plain-text community strings. For production security:
    * Add SNMPv3 support with authentication and encryption
    * Make SNMPv3 the recommended configuration
    * Keep SNMPv2c for backward compatibility only

---

## Section to Add: Performance Optimization Guidelines

**Insert after "Architecture & Implementation Details":**

## Performance Optimization Guidelines

### Batch Operations
* **InfluxDB Writes:** Batch multiple data points before writing to InfluxDB
    * Default batch size: 100 points
    * Default flush interval: 5 seconds
    * Configurable via `influxdb.batch_size` and `influxdb.flush_interval`
* **SNMP Queries:** Group OIDs when querying same device to reduce round-trips
* **Network Operations:** Use worker pools efficiently (already implemented)

### Resource Management
* **Adaptive Workers:** Consider auto-tuning worker counts based on:
    * Available CPU cores (`runtime.NumCPU()`)
    * Network latency
    * Current load
* **Circuit Breakers:** For devices that consistently fail:
    * Stop trying after N consecutive failures
    * Exponential backoff before retry
    * Remove from active monitoring (optionally)

### Memory Optimization
* **Device Eviction:** Already implemented (LRU-style eviction)
* **Goroutine Pooling:** Reuse goroutines where possible
* **Buffer Sizing:** Use appropriate channel buffer sizes (currently 256)

---

## Section to Add: Production Readiness Checklist

**Insert after "How to Add a New Feature":**

## Production Readiness Checklist

Before deploying to production, ensure:

### Essential (MUST HAVE)
- [ ] Health check endpoint implemented and tested
- [ ] All critical metrics being tracked
- [ ] Structured logging in place
- [ ] Resource limits configured appropriately
- [ ] InfluxDB connection resilience tested
- [ ] Graceful shutdown working correctly
- [ ] Configuration validation in place
- [ ] Security scanning passing (no HIGH/CRITICAL CVEs)

### Recommended (SHOULD HAVE)
- [ ] Integration tests passing
- [ ] Performance benchmarks established
- [ ] Multi-architecture builds available
- [ ] Kubernetes manifests available (if applicable)
- [ ] Monitoring dashboard configured (Grafana)
- [ ] Alert rules configured (InfluxDB/Grafana)
- [ ] Operational runbook documented
- [ ] Disaster recovery plan documented

### Optional (NICE TO HAVE)
- [ ] SNMPv3 support enabled
- [ ] State persistence configured
- [ ] IPv6 support enabled
- [ ] Device grouping/tagging implemented
- [ ] Webhook alerting configured
- [ ] Secrets management integrated

---

## Section to Update: Technology Stack

**Update the "Technology Stack" section:**

### Current Content:
```markdown
* **Language**: Go 1.25+ (updated from 1.21 - ensure Dockerfile uses golang:1.25-alpine)
```

### Recommended Addition:
```markdown
* **Language**: Go 1.25+ (updated from 1.21 - ensure Dockerfile uses golang:1.25-alpine)
* **Recommended Additional Libraries**:
    * `github.com/prometheus/client_golang` (Metrics collection)
    * `github.com/rs/zerolog` or `go.uber.org/zap` (Structured logging)
    * `golang.org/x/time/rate` (Already used, document it)
    * `golang.org/x/vuln/cmd/govulncheck` (Vulnerability scanning)
```

---

## Section to Update: CI/CD Requirements

**Update the "CI/CD Requirements" section:**

### Current Content:
```markdown
* `./netscan --help` must work (flag support required)
* All unit tests must pass
* Race detection must be clean: `go test -race ./...`
* Build must succeed with no warnings
* Docker Compose workflow validates full stack deployment
* Workflow creates config.yml from template and runs `docker compose up`
```

### Recommended Additions:
```markdown
* `./netscan --help` must work (flag support required)
* All unit tests must pass
* All integration tests must pass (if applicable)
* Race detection must be clean: `go test -race ./...`
* Build must succeed with no warnings
* **Security scanning must pass (govulncheck, Trivy)**
* **Performance benchmarks must not regress**
* Docker Compose workflow validates full stack deployment
* Workflow creates config.yml from template and runs `docker compose up`
* **Multi-architecture Docker images must build successfully**
```

---

## Section to Add: Upgrade and Migration Guidelines

**Insert as new section:**

## Upgrade and Migration Guidelines

### Version Compatibility
* **Configuration backward compatibility:** New fields must have sensible defaults
* **State format changes:** Must support migration from previous version
* **Breaking changes:** Require major version bump and migration guide

### Upgrade Procedure
1. **Review changelog** for breaking changes
2. **Backup current state** (if persistence enabled)
3. **Update configuration** with new fields (optional)
4. **Test in staging** environment first
5. **Rolling update** in production (if multiple instances)
6. **Monitor logs** for errors after upgrade
7. **Rollback plan** ready if issues occur

### Migration Scripts
* Provide migration scripts for breaking changes
* Document manual migration steps clearly
* Test migration with realistic data volumes

---

## Section to Add: Troubleshooting Guide Additions

**Add to existing troubleshooting section:**

### Advanced Troubleshooting

**High Memory Usage:**
```bash
# Check goroutine count
curl http://localhost:8080/debug/pprof/goroutine?debug=1

# Check memory profile
curl http://localhost:8080/debug/pprof/heap > heap.prof
go tool pprof heap.prof
```

**Slow Scans:**
```bash
# Check active pinger count
curl http://localhost:8080/health | jq '.active_pingers'

# Check if worker pool is saturated
# Look for "worker pool full" messages in logs

# Adjust worker counts in config.yml
```

**InfluxDB Write Failures:**
```bash
# Check InfluxDB health
curl http://localhost:8086/health

# Check write errors in metrics
curl http://localhost:8080/metrics | grep influx_write_errors

# Check rate limiter status
# Look for "rate limit exceeded" in logs
```

---

## Section to Add: Code Examples for Common Patterns

**Insert as new section:**

## Common Implementation Patterns

### Adding a New Ticker
```go
// 1. Add configuration parameter
type Config struct {
    NewScannerInterval time.Duration `yaml:"new_scanner_interval"`
}

// 2. Create ticker in main.go
newScannerTicker := time.NewTicker(cfg.NewScannerInterval)
defer newScannerTicker.Stop()

// 3. Add to main event loop
case <-newScannerTicker.C:
    // Your scan logic here
    log.Println("Starting new scanner...")
```

### Adding a New Metric
```go
// 1. Define metric in metrics package
var DevicesScanned = prometheus.NewCounterVec(
    prometheus.CounterOpts{
        Name: "netscan_devices_scanned_total",
        Help: "Total number of devices scanned",
    },
    []string{"scan_type"},
)

// 2. Register metric
func init() {
    prometheus.MustRegister(DevicesScanned)
}

// 3. Increment metric
DevicesScanned.WithLabelValues("icmp").Inc()
```

### Adding a New State Field
```go
// 1. Update Device struct
type Device struct {
    IP       string
    NewField string  // Add your field
    LastSeen time.Time
}

// 2. Add update method
func (m *Manager) UpdateNewField(ip, value string) {
    m.mu.Lock()
    defer m.mu.Unlock()
    if dev, exists := m.devices[ip]; exists {
        dev.NewField = value
    }
}

// 3. Update tests
func TestUpdateNewField(t *testing.T) {
    // Test the new method
}
```

---

## Priority Implementation Order

Based on the comprehensive analysis, implement improvements in this order:

### Phase 1: Critical (Weeks 1-2)
1. Health check endpoint
2. Security vulnerability scanning in CI/CD
3. Tests for main orchestration logic
4. Structured logging framework

### Phase 2: High Priority (Weeks 3-4)
5. Metrics collection infrastructure
6. Batch InfluxDB writes
7. Multi-architecture Docker builds
8. SNMPv3 support foundation

### Phase 3: Medium Priority (Month 2)
9. Performance benchmarks
10. Integration test suite
11. Circuit breaker pattern
12. Configuration reload capability

### Phase 4: Future Enhancements (Month 3+)
13. IPv6 support
14. State persistence
15. Device groups and tags
16. Kubernetes manifests and Helm chart

---

## Summary of Changes to Copilot Instructions

**Additions:**
- Observability & Monitoring Mandates section
- Enhanced Testing Mandates section
- Security Scanning requirements
- Performance Optimization Guidelines section
- Production Readiness Checklist section
- Upgrade and Migration Guidelines section
- Advanced Troubleshooting section
- Common Implementation Patterns section

**Updates:**
- Technology Stack (add recommended libraries)
- CI/CD Requirements (add security and benchmark requirements)
- Troubleshooting section (add advanced debugging)

**Result:** More comprehensive guidance for developers while maintaining the excellent architectural foundation and clear boundaries already established.
