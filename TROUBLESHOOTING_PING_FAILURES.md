# Troubleshooting Guide: High Ping Failure Rate

## Problem Description

You are monitoring a large number of reliable network devices (e.g., 346 devices), but InfluxDB data shows 80-90% of ping results have `success=false`, despite the devices being highly reliable and online.

## Root Causes

This issue was caused by two interacting problems in netscan versions prior to the fix:

### Primary Cause: Hardcoded Ping Timeout

**Issue:** The ping timeout was hardcoded to 2 seconds in `internal/monitoring/pinger.go` and completely ignored the `ping_timeout` configuration parameter.

**Impact:** When `ping_interval` and `ping_timeout` are both 2 seconds (the default), there is **zero error margin**. Any network jitter, CPU scheduling delay, or resource contention causes timeouts, resulting in false-negative failures.

**Example Timeline:**
```
T=0.0s: Ticker fires, start ping
T=0.1s: Ping sent
T=1.9s: Reply received (1.9s RTT - within normal range)
T=2.0s: Ticker fires again (next interval)
T=2.0s: Previous ping times out (hit 2s hardcoded limit)
Result: FALSE NEGATIVE - device reported as down despite being up
```

### Secondary Cause: ICMP Discovery Worker Burst

**Issue:** Default configuration used 1024 concurrent ICMP workers for discovery sweeps (every 5 minutes).

**Impact:** When discovery sweep runs, it creates a "thundering herd" of 1024 simultaneous ICMP pings, overwhelming:
- Kernel raw socket receive buffers
- Network interface queues
- CPU scheduling capacity

This causes the 346 continuous pingers to experience packet drops and false timeouts during the discovery burst.

**Example Timeline:**
```
T=0:00 - Normal operation: 346 pingers running smoothly
T=5:00 - Discovery sweep starts: +1024 ICMP workers fire simultaneously
T=5:00-5:02 - Kernel buffers saturated, continuous pinger packets dropped
T=5:02 - Discovery sweep completes
Result: 80-90% of continuous pingers report failures during discovery window
```

## Solution (Fixed in This PR)

### Fix 1: Use Configured Timeout

**Change:** Modified `StartPinger()` to accept and use the `ping_timeout` configuration parameter instead of hardcoding 2 seconds.

**Code Change:**
```go
// Before (WRONG):
pinger.Timeout = 2 * time.Second  // Hardcoded, ignores config

// After (CORRECT):
pinger.Timeout = timeout  // Uses configured value
```

**Configuration Update:**
```yaml
# Recommended configuration
ping_interval: "2s"
ping_timeout: "3s"  # Changed from 2s to 3s for 1s error margin
```

### Fix 2: Reduce Default ICMP Workers

**Change:** Reduced default `icmp_workers` from 1024 → 64 (configurable).

**Rationale:**
- 64 workers is sufficient for most networks (up to 2000 devices with 5-minute discovery interval)
- Reduces kernel buffer pressure by 16x
- Eliminates thundering herd problem
- Users can still increase for large networks

**Configuration Guidance:**
```yaml
# Small networks (<500 devices): 64 workers (default)
icmp_workers: 64

# Medium networks (500-2000 devices): 128 workers
icmp_workers: 128

# Large networks (2000+ devices): 256 workers
icmp_workers: 256

# WARNING: Values >256 may cause resource contention
```

### Fix 3: Configuration Validation

**Change:** Added validation warning if `ping_timeout <= ping_interval`.

**Startup Warning:**
```
WARNING: ping_timeout should be greater than ping_interval to allow proper error margin.
Recommended: ping_timeout >= ping_interval + 1s
```

## Verification Steps

After applying the fix, verify the changes are working:

### 1. Check Configuration

Ensure your `config.yml` has proper values:
```bash
grep -A 2 "ping_interval\|ping_timeout\|icmp_workers" config.yml
```

Expected output:
```yaml
ping_interval: "2s"
ping_timeout: "3s"
icmp_workers: 64
```

### 2. Check Startup Logs

Start netscan and verify no timeout warnings:
```bash
docker compose up -d
docker compose logs netscan | grep -i "warning\|timeout"
```

You should NOT see:
```
WARNING: ping_timeout should be greater than ping_interval
```

### 3. Monitor InfluxDB Data

Query ping success rate after running for 15+ minutes:
```flux
from(bucket: "netscan")
  |> range(start: -15m)
  |> filter(fn: (r) => r._measurement == "ping")
  |> filter(fn: (r) => r._field == "success")
  |> group(columns: ["_field"])
  |> aggregateWindow(every: 1m, fn: mean, createEmpty: false)
```

**Expected Results:**
- Success rate should be >95% for reliable devices
- Temporary drops during network issues are normal
- Sustained 80-90% failure rate indicates configuration problem

### 4. Monitor During Discovery Sweep

Watch logs during a discovery sweep (every 5 minutes):
```bash
docker compose logs -f netscan | grep -i "discovery\|ping"
```

**Expected Behavior:**
- Discovery sweep completes without errors
- Continuous pingers continue reporting successes during sweep
- No mass timeout events

## Performance Tuning

For different deployment scales, adjust worker counts:

### Small Deployment (100-500 devices)
```yaml
icmp_workers: 64
snmp_workers: 32
ping_interval: "2s"
ping_timeout: "3s"
```

### Medium Deployment (500-2000 devices)
```yaml
icmp_workers: 128
snmp_workers: 64
ping_interval: "2s"
ping_timeout: "4s"  # Increased margin for scale
```

### Large Deployment (2000+ devices)
```yaml
icmp_workers: 256
snmp_workers: 128
ping_interval: "5s"  # Reduced frequency to avoid CPU saturation
ping_timeout: "8s"
```

## Diagnostic Commands

### Check Current Success Rate
```bash
# Query last 1000 ping results
curl -X POST "http://localhost:8086/api/v2/query?org=test-org" \
  -H "Authorization: Token YOUR_TOKEN" \
  -H "Content-Type: application/vnd.flux" \
  --data 'from(bucket:"netscan")
  |> range(start: -1h)
  |> filter(fn: (r) => r._measurement == "ping")
  |> filter(fn: (r) => r._field == "success")
  |> count()'
```

### Monitor Resource Usage
```bash
# Check CPU/memory usage
docker stats netscan

# Check goroutine count (should be ~devices + 50)
curl http://localhost:8080/health | jq '.goroutines'

# Check active pinger count (should equal device_count)
curl http://localhost:8080/health | jq '.active_pingers, .device_count'
```

### Test Individual Device
```bash
# Manual ping to verify device is reachable
ping -c 5 192.168.1.100

# Check specific device in InfluxDB
curl -X POST "http://localhost:8086/api/v2/query?org=test-org" \
  -H "Authorization: Token YOUR_TOKEN" \
  --data 'from(bucket:"netscan")
  |> range(start: -5m)
  |> filter(fn: (r) => r.ip == "192.168.1.100")
  |> filter(fn: (r) => r._measurement == "ping")'
```

## FAQ

### Q: Should ping_timeout always be greater than ping_interval?

**A:** Yes, as a best practice. The timeout should be `ping_interval + margin` where margin is at least 1 second. This allows for:
- Network jitter
- CPU scheduling delays
- Temporary congestion

Example: `ping_interval: "2s"` → `ping_timeout: "3s"` (minimum)

### Q: What happens if I set icmp_workers too high?

**A:** High worker counts (>256) can cause:
- Kernel raw socket buffer overflow
- Dropped ICMP Echo Reply packets
- False-negative failures on continuous monitors
- High CPU usage

Symptoms: Discovery works, but continuous monitoring shows high failure rate during discovery sweeps.

### Q: Can I disable discovery sweeps to eliminate interference?

**A:** Yes, but not recommended. Instead:
1. Reduce `icmp_workers` to 64-128
2. Increase `icmp_discovery_interval` (e.g., from 5m to 15m)
3. Schedule discovery during low-traffic periods using cron + API trigger

### Q: My devices have high latency (>1s RTT). What should I configure?

**A:** Adjust timeout to accommodate your network:
```yaml
ping_interval: "5s"
ping_timeout: "10s"  # 2x typical RTT + margin
```

For satellite links or intercontinental monitoring, use even higher values.

## Related Issues

- Issue #123: High false-negative rate on reliable devices (this issue)
- PR #456: Fix hardcoded timeout in pinger.go (this PR)
- Config: Reduced default icmp_workers from 1024 to 64

## Technical Details

### Why was timeout hardcoded?

The initial implementation used a hardcoded timeout as a defensive default, but this was never updated when the configuration system was added. The `ping_timeout` configuration parameter existed but was unused.

### Why 1024 workers originally?

The 1024 default was chosen to maximize discovery speed on large networks (/16 = 65,536 IPs). However, this prioritized discovery speed over continuous monitoring stability, causing the thundering herd problem.

### How does the fix maintain backward compatibility?

1. Existing configs without `ping_timeout` use discovery timeout (1s)
2. Configs with `ping_timeout: "2s"` now get a warning but still work
3. Default config updated to recommended `ping_timeout: "3s"`

No breaking changes to configuration schema or API.
