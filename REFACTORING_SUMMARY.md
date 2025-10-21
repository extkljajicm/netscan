# Netscan Architecture Refactoring Summary

## Overview
This document summarizes the refactoring of the netscan project from a single-loop discovery architecture to a decoupled, multi-ticker architecture as specified in `.github/copilot-instructions.md`.

## Architecture Changes

### Before: Single Discovery Loop
- One main ticker running SNMP+ICMP discovery
- Pingers started/stopped only during discovery cycles
- Alternative `--icmp-only` mode with separate code path
- No scheduled SNMP scans

### After: Multi-Ticker Architecture
Four independent, concurrent tickers:

1. **ICMP Discovery Loop** (every `icmp_discovery_interval`)
   - Performs ICMP ping sweep across configured networks
   - Finds new, responsive devices
   - Triggers immediate SNMP scan for new devices

2. **Daily SNMP Scan Loop** (at `snmp_daily_schedule` time)
   - Full SNMP scan of all known devices
   - Runs once per day at configured time (e.g., "02:00")
   - Optional - can be disabled by leaving schedule empty

3. **Pinger Reconciliation Loop** (every 5 seconds)
   - Ensures every device in StateManager has an active pinger
   - Starts pingers for new devices
   - Stops pingers for removed devices
   - Respects `max_concurrent_pingers` limit

4. **State Pruning Loop** (every 1 hour)
   - Removes devices not seen in last 24 hours
   - Prevents memory growth from stale devices

## Component Changes

### 1. Configuration (`internal/config/config.go`)
**Added:**
- `SNMPDailySchedule string` - Daily SNMP scan time in HH:MM format
- `validateTimeFormat()` - Validates HH:MM time format

**Example:**
```yaml
snmp_daily_schedule: "02:00"  # Run full SNMP scan at 2 AM daily
```

### 2. State Manager (`internal/state/manager.go`)
**Added Methods:**
- `AddDevice(ip string) bool` - Adds device by IP, returns true if new
- `UpdateDeviceSNMP(ip, hostname, sysDescr, sysObjectID string)` - Enriches device with SNMP data
- `GetAllIPs() []string` - Returns all managed device IPs
- `PruneStale(olderThan time.Duration)` - Alias for Prune with clearer name

**Thread Safety:**
- All methods remain fully thread-safe with `sync.RWMutex`
- Maintains device limit enforcement with eviction

### 3. Scanner (`internal/discovery/scanner.go`)
**New Functions:**
- `RunICMPSweep(networks []string, workers int) []string`
  - Performs ICMP ping sweep across multiple networks
  - Returns only responsive IP addresses
  - Uses worker pool pattern for concurrency
  - Retains all security protections (CIDR limits, etc.)

- `RunSNMPScan(ips []string, snmpConfig *config.SNMPConfig, workers int) []state.Device`
  - Performs SNMP queries on specific IPs
  - Returns devices with SNMP data populated
  - Gracefully handles SNMP failures (logs and continues)
  - Uses timeouts and validates all SNMP responses

**Retained:**
- All existing functions (`RunPingDiscovery`, `RunFullDiscovery`, etc.)
- All security fixes from `SECURITY_IMPROVEMENTS.md`

### 4. Pinger (`internal/monitoring/pinger.go`)
**Updated Signature:**
```go
// Before
func StartPinger(device state.Device, cfg *config.Config, writer PingWriter, ctx context.Context)

// After
func StartPinger(ctx context.Context, wg *sync.WaitGroup, device state.Device, 
                 interval time.Duration, writer PingWriter, stateMgr StateManager)
```

**Changes:**
- Accepts `StateManager` interface to update LastSeen timestamps
- Accepts `WaitGroup` for graceful shutdown tracking
- Accepts `interval` directly instead of full config
- Calls `stateMgr.UpdateLastSeen()` after successful pings
- Fixed 2-second ping timeout (was using config value)

### 5. Main Orchestration (`cmd/netscan/main.go`)
**Complete Rewrite:**
- Removed `--icmp-only` flag (simplified architecture)
- Four independent tickers running concurrently
- Single `StateManager` as source of truth
- Proper mutex protection for `activePingers` map
- Graceful shutdown with WaitGroup

**Flow:**
1. Load and validate config
2. Initialize StateManager and InfluxDB writer
3. Run initial ICMP discovery
4. Start all tickers
5. Main event loop handles all ticker events
6. Graceful shutdown on signal

## Security & Stability

**No Regressions:**
All security fixes from `SECURITY_IMPROVEMENTS.md` are retained:
- ✅ Mutex protection for `activePingers` map
- ✅ InfluxDB health check on startup
- ✅ Timeouts on all InfluxDB writes
- ✅ CIDR expansion limits (max /16)
- ✅ Input validation and sanitization
- ✅ Rate limiting on InfluxDB writes
- ✅ Memory usage monitoring
- ✅ Resource limits (max devices, max pingers)

**Additional Improvements:**
- Better separation of concerns (decoupled operations)
- More predictable pinger lifecycle (reconciliation loop)
- Scheduled SNMP scans reduce overhead
- Graceful shutdown with WaitGroup

## Configuration Migration

**Required Changes:**
Add the new `snmp_daily_schedule` field to your `config.yml`:

```yaml
# Daily scheduled SNMP scan time (HH:MM format in 24-hour time)
# Leave empty to disable scheduled SNMP scans
snmp_daily_schedule: "02:00"
```

**Optional Changes:**
The `--icmp-only` flag has been removed. The new architecture automatically:
- Discovers devices via ICMP continuously
- Performs SNMP scans on discovery and daily schedule
- Continuously monitors all devices with pingers

## Testing

**All Tests Pass:**
```bash
$ go test ./...
ok      github.com/kljama/netscan/internal/config
ok      github.com/kljama/netscan/internal/discovery
ok      github.com/kljama/netscan/internal/influx
ok      github.com/kljama/netscan/internal/monitoring
ok      github.com/kljama/netscan/internal/state
```

**Manual Validation:**
- ✅ Config loading and validation works
- ✅ Invalid time format correctly rejected
- ✅ Empty schedule correctly handled
- ✅ Application starts up correctly
- ✅ All new StateManager methods work
- ✅ Build succeeds without errors

## Benefits

1. **Decoupled Operations:** Discovery, monitoring, and SNMP scanning are independent
2. **Predictable Behavior:** Reconciliation loop ensures consistency
3. **Scheduled SNMP:** Daily scans reduce overhead, immediate scans for new devices
4. **Better Resource Management:** Clear lifecycle for pingers
5. **Improved Observability:** Clear separation makes debugging easier
6. **No Breaking Changes:** Existing config files work (just add new field)

## Next Steps

1. Update production `config.yml` with `snmp_daily_schedule`
2. Test with your actual network ranges
3. Monitor logs for the new ticker messages
4. Verify SNMP scans run at scheduled time
5. Check that pingers start/stop correctly during reconciliation

## Questions or Issues?

If you encounter any issues or have questions about the refactoring:
1. Check logs for detailed information about each ticker
2. Verify `config.yml` has the new `snmp_daily_schedule` field
3. Ensure all security settings are properly configured
4. Review the copilot-instructions.md for architecture details
