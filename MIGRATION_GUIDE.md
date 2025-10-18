# Migration Guide: Single-Loop to Multi-Ticker Architecture

## Quick Start

The refactoring is **backward compatible** with one required config addition:

### Required Change

Add this to your `config.yml`:

```yaml
# Daily scheduled SNMP scan time (HH:MM format in 24-hour time)
# Full SNMP scan of all known devices runs once per day at this time
# Leave empty to disable scheduled SNMP scans
snmp_daily_schedule: "02:00"
```

### Optional Changes

No other changes are required! Your existing configuration will work as-is.

## What Changed

### Architecture

**Old Behavior:**
- Single discovery loop running periodically
- SNMP+ICMP discovery every `discovery_interval`
- Pingers started/stopped during discovery
- `--icmp-only` flag for alternative mode

**New Behavior:**
- Four independent tickers running concurrently:
  1. **ICMP Discovery** (every `icmp_discovery_interval`) - Finds new devices
  2. **Daily SNMP Scan** (at `snmp_daily_schedule`) - Enriches all devices
  3. **Pinger Reconciliation** (every 5s) - Ensures all devices monitored
  4. **State Pruning** (every 1h) - Removes stale devices
- Removed `--icmp-only` flag (no longer needed)

### Benefits

✅ **More Efficient** - ICMP runs frequently, SNMP runs on-demand and scheduled  
✅ **More Reliable** - Reconciliation ensures no missed devices  
✅ **More Predictable** - Clear separation of concerns  
✅ **More Observable** - Each ticker logs independently  

## Behavior Changes

### Device Discovery

**Before:**
```
1. ICMP + SNMP discovery runs every 4h
2. New devices get SNMP data during discovery
3. Pingers start during discovery
```

**After:**
```
1. ICMP discovery runs every 5m (finds responsive devices)
2. New devices trigger immediate SNMP scan (enrichment)
3. Daily SNMP scan at 02:00 refreshes all devices
4. Reconciliation ensures all devices have pingers (every 5s)
```

### Pinger Lifecycle

**Before:**
- Started: During discovery when device found
- Stopped: During discovery when device not seen 2x intervals

**After:**
- Started: Reconciliation loop detects new device in StateManager
- Stopped: Reconciliation loop detects device removed from StateManager
- Device removed: Pruning loop removes devices not seen in 24h

### SNMP Scanning

**Before:**
- Full SNMP scan every `discovery_interval` (e.g., 4h)
- All devices scanned together

**After:**
- **Immediate:** New devices get SNMP scan right away
- **Scheduled:** All devices get SNMP scan at `snmp_daily_schedule` (e.g., 02:00)
- **Benefit:** Less overhead, faster response to new devices

## Migration Steps

### Step 1: Update Config

Edit your `config.yml`:

```yaml
# Add this new field
snmp_daily_schedule: "02:00"  # Or your preferred time in HH:MM format

# Keep all existing fields
discovery_interval: "4h"           # Still used for compatibility
icmp_discovery_interval: "5m"      # Now primary discovery mechanism
# ... rest of your config ...
```

### Step 2: Deploy New Binary

```bash
# Build the new version
go build ./cmd/netscan

# Stop the old service
sudo systemctl stop netscan

# Deploy new binary
sudo cp netscan /usr/local/bin/

# Start the new service
sudo systemctl start netscan
```

### Step 3: Verify Operation

Check logs for the new ticker messages:

```bash
# Should see these at startup:
tail -f /var/log/netscan.log | grep -E "Starting|Reconciliation|Pruning|SNMP"
```

Expected log patterns:
```
Starting monitoring loops...
- ICMP Discovery: every 5m0s
- Daily SNMP Scan: at 02:00
- Pinger Reconciliation: every 5s
- State Pruning: every 1h

Starting ICMP discovery scan...
ICMP discovery found X online devices
New device found: 192.168.1.1. Performing initial SNMP scan.
Device enriched: 192.168.1.1 (router-01)
Starting continuous pinger for 192.168.1.1

# Every 5 seconds (only if changes):
Starting continuous pinger for 192.168.1.5
Stopping continuous pinger for stale device 192.168.1.99

# At configured time (e.g., 02:00):
Starting daily full SNMP scan...
Performing SNMP scan on 150 devices...
SNMP scan complete, enriched 148 devices
```

### Step 4: Monitor Performance

Watch for:
- ✅ Devices discovered more quickly (ICMP runs every 5m)
- ✅ Lower SNMP overhead (once daily vs every 4h)
- ✅ Consistent pinger counts (reconciliation ensures correctness)
- ✅ Memory stable (pruning prevents growth)

## Troubleshooting

### "Invalid configuration: snmp_daily_schedule validation failed"

**Cause:** Invalid time format in config  
**Solution:** Use HH:MM format with valid hours (00-23) and minutes (00-59)

```yaml
# ✅ Correct
snmp_daily_schedule: "02:00"
snmp_daily_schedule: "14:30"

# ❌ Incorrect
snmp_daily_schedule: "2:00"    # Missing leading zero
snmp_daily_schedule: "25:00"   # Invalid hour
snmp_daily_schedule: "14:60"   # Invalid minute
```

### Devices Not Getting SNMP Data

**Check:**
1. SNMP credentials in config are correct
2. Devices respond to SNMP queries
3. Check logs for "SNMP connection failed" or "SNMP query failed"
4. Verify scheduled scan time has passed or wait for next cycle

**Debug:**
```bash
# Check if SNMP scan ran
grep "Starting daily full SNMP scan" /var/log/netscan.log

# Check for SNMP errors
grep "SNMP.*failed" /var/log/netscan.log
```

### Too Many Pingers Starting/Stopping

**Cause:** Devices frequently appearing/disappearing from StateManager  
**Check:**
1. Network stability (are devices really going offline?)
2. Ping timeout setting (may be too short)
3. Pruning interval (24h default, may need adjustment)

**Solution:** Review network stability before adjusting timeouts

### High Memory Usage

**Check:**
1. `max_devices` limit in config
2. Number of networks being scanned
3. Pruning is working (check logs for "Pruning stale devices")

**Debug:**
```bash
# Check pruning logs
grep "Pruning" /var/log/netscan.log

# Monitor memory
watch -n 5 'ps aux | grep netscan'
```

## Rollback Plan

If you need to rollback to the old version:

1. Keep the old binary: `cp /usr/local/bin/netscan /usr/local/bin/netscan.old`
2. If issues occur: `cp /usr/local/bin/netscan.old /usr/local/bin/netscan`
3. Remove `snmp_daily_schedule` from config.yml
4. Restart service: `sudo systemctl restart netscan`

The old version is still compatible with your config (will ignore new field).

## FAQ

**Q: Do I need to change my config?**  
A: Yes, add `snmp_daily_schedule`. Everything else stays the same.

**Q: Will my existing data in InfluxDB be affected?**  
A: No, data schema is unchanged. New data will be written normally.

**Q: Can I disable the daily SNMP scan?**  
A: Yes, set `snmp_daily_schedule: ""` (empty string).

**Q: What happened to `--icmp-only` flag?**  
A: Removed. The new architecture handles both ICMP and SNMP efficiently.

**Q: Are there any breaking changes?**  
A: No breaking changes. Just add the new config field.

**Q: How do I test before deploying to production?**  
A: Run in a test environment first:
```bash
# Test config validation
./netscan --help  # Will exit after loading config

# Or test with a small network
networks:
  - "192.168.1.0/29"  # Only 8 IPs
```

**Q: Will this affect my Grafana dashboards?**  
A: No, InfluxDB measurements remain the same (`ping`, `device_info`).

## Support

For issues or questions:
1. Check logs first: `tail -f /var/log/netscan.log`
2. Review `REFACTORING_SUMMARY.md` for detailed changes
3. Verify config with a small test network first
4. Check `.github/copilot-instructions.md` for architecture details

## Summary

✅ **One Required Change:** Add `snmp_daily_schedule` to config  
✅ **Zero Breaking Changes:** Existing configs work as-is  
✅ **Better Performance:** More efficient discovery and monitoring  
✅ **Easy Rollback:** Keep old binary just in case  

The refactoring improves architecture while maintaining compatibility! 🎉
