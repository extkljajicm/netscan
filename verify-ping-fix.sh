#!/bin/bash
# Verification script for ping timeout fix
# This script demonstrates the fix and helps users verify their configuration

set -e

echo "==================================================================="
echo "Netscan Ping Timeout Fix Verification"
echo "==================================================================="
echo ""

# Check if config.yml exists
if [ ! -f "config.yml" ]; then
    echo "❌ ERROR: config.yml not found"
    echo "   Run: cp config.yml.example config.yml"
    exit 1
fi

echo "✅ Found config.yml"
echo ""

# Extract current configuration values
PING_INTERVAL=$(grep "^ping_interval:" config.yml | awk '{print $2}' | tr -d '"')
PING_TIMEOUT=$(grep "^ping_timeout:" config.yml | awk '{print $2}' | tr -d '"')
ICMP_WORKERS=$(grep "^icmp_workers:" config.yml | awk '{print $2}')
SNMP_WORKERS=$(grep "^snmp_workers:" config.yml | awk '{print $2}')

echo "Current Configuration:"
echo "  ping_interval:  $PING_INTERVAL"
echo "  ping_timeout:   $PING_TIMEOUT"
echo "  icmp_workers:   $ICMP_WORKERS"
echo "  snmp_workers:   $SNMP_WORKERS"
echo ""

# Function to convert duration string to seconds (handles simple cases)
duration_to_seconds() {
    local dur=$1
    # Remove quotes if present
    dur=$(echo "$dur" | tr -d '"')
    
    # Handle simple cases: Xs, Xm, Xh
    if [[ "$dur" =~ ^([0-9]+)s$ ]]; then
        echo "${BASH_REMATCH[1]}"
    elif [[ "$dur" =~ ^([0-9]+)m$ ]]; then
        echo $((BASH_REMATCH[1] * 60))
    elif [[ "$dur" =~ ^([0-9]+)h$ ]]; then
        echo $((BASH_REMATCH[1] * 3600))
    else
        # Complex duration (e.g., "1h30m") - not supported by this simple parser
        # Return -1 to indicate parsing failure
        echo "-1"
    fi
}

INTERVAL_SEC=$(duration_to_seconds "$PING_INTERVAL")
TIMEOUT_SEC=$(duration_to_seconds "$PING_TIMEOUT")

echo "==================================================================="
echo "Configuration Analysis"
echo "==================================================================="
echo ""

# Check 1: Timeout vs Interval
# Skip timeout comparison if parsing failed (complex duration format)
if [ "$INTERVAL_SEC" -eq -1 ] || [ "$TIMEOUT_SEC" -eq -1 ]; then
    echo "⚠️  NOTE: Complex duration format detected, skipping timeout comparison"
    echo "   Please manually verify: ping_timeout > ping_interval"
    echo ""
elif [ "$TIMEOUT_SEC" -le "$INTERVAL_SEC" ]; then
    echo "❌ WARNING: ping_timeout ($PING_TIMEOUT) <= ping_interval ($PING_INTERVAL)"
    echo "   This creates zero error margin and will cause high failure rates."
    echo "   RECOMMENDATION: Set ping_timeout to at least ping_interval + 1s"
    echo "   Example: ping_timeout: \"3s\" (for ping_interval: \"2s\")"
    echo ""
    FIX_NEEDED=1
elif [ "$TIMEOUT_SEC" -eq $((INTERVAL_SEC + 1)) ]; then
    echo "✅ OK: ping_timeout ($PING_TIMEOUT) = ping_interval + 1s (minimal margin)"
    echo "   This is the minimum recommended configuration."
    echo ""
else
    echo "✅ GOOD: ping_timeout ($PING_TIMEOUT) > ping_interval ($PING_INTERVAL)"
    echo "   Error margin: $((TIMEOUT_SEC - INTERVAL_SEC))s"
    echo ""
fi

# Check 2: ICMP Workers
if [ "$ICMP_WORKERS" -gt 256 ]; then
    echo "❌ WARNING: icmp_workers ($ICMP_WORKERS) is very high"
    echo "   High worker counts (>256) can overwhelm kernel raw socket buffers."
    echo "   RECOMMENDATION: Reduce to 64-128 for most networks"
    echo "   See TROUBLESHOOTING_PING_FAILURES.md for tuning guidance"
    echo ""
    FIX_NEEDED=1
elif [ "$ICMP_WORKERS" -gt 128 ]; then
    echo "⚠️  CAUTION: icmp_workers ($ICMP_WORKERS) is moderately high"
    echo "   This is OK for large networks (2000+ devices) with fast hardware."
    echo "   If you experience high ping failure rates, reduce to 64-128."
    echo ""
elif [ "$ICMP_WORKERS" -ge 32 ]; then
    echo "✅ GOOD: icmp_workers ($ICMP_WORKERS) is in recommended range"
    echo ""
else
    echo "⚠️  NOTE: icmp_workers ($ICMP_WORKERS) is quite low"
    echo "   This may slow down discovery on large networks."
    echo "   For 500+ devices, consider increasing to 64-128."
    echo ""
fi

# Check 3: Build verification
echo "==================================================================="
echo "Build Verification"
echo "==================================================================="
echo ""

if [ -f "netscan" ]; then
    echo "✅ Found compiled binary: netscan"
    
    # Check if binary was built after the fix
    BUILD_DATE=$(stat -c %y netscan 2>/dev/null || stat -f "%Sm" -t "%Y-%m-%d %H:%M:%S" netscan 2>/dev/null || echo "unknown")
    echo "   Build date: $BUILD_DATE"
    echo ""
    
    # Verify the fix is present by checking the binary
    if strings netscan | grep -q "ping_timeout should be greater than ping_interval"; then
        echo "✅ VERIFIED: Binary contains timeout validation code (fix is present)"
        echo ""
    else
        echo "❌ WARNING: Binary may be from before the fix"
        echo "   Rebuild with: go build -o netscan ./cmd/netscan"
        echo ""
    fi
else
    echo "❌ Binary not found. Build with:"
    echo "   go build -o netscan ./cmd/netscan"
    echo ""
fi

# Check 4: Test suite
echo "==================================================================="
echo "Test Suite Verification"
echo "==================================================================="
echo ""

if command -v go &> /dev/null; then
    echo "Running timeout configuration tests..."
    echo ""
    
    if go test ./internal/monitoring -run TestTimeoutParameterPropagation -v 2>&1 | grep -q "PASS"; then
        echo "✅ TestTimeoutParameterPropagation: PASSED"
        echo "   Confirms timeout parameter is properly propagated"
    else
        echo "❌ TestTimeoutParameterPropagation: FAILED"
    fi
    
    if go test ./internal/monitoring -run TestTimeoutNotHardcoded -v 2>&1 | grep -q "PASS"; then
        echo "✅ TestTimeoutNotHardcoded: PASSED"
        echo "   Confirms timeout is not hardcoded to 2s"
    else
        echo "❌ TestTimeoutNotHardcoded: FAILED"
    fi
    
    if go test ./internal/config -run TestValidateConfigTimeoutWarning -v 2>&1 | grep -q "PASS"; then
        echo "✅ TestValidateConfigTimeoutWarning: PASSED"
        echo "   Confirms validation warnings work correctly"
    else
        echo "❌ TestValidateConfigTimeoutWarning: FAILED"
    fi
    echo ""
else
    echo "⚠️  Go not installed, skipping test verification"
    echo ""
fi

# Summary
echo "==================================================================="
echo "Summary"
echo "==================================================================="
echo ""

if [ "${FIX_NEEDED:-0}" -eq 1 ]; then
    echo "❌ Configuration issues detected. Please review warnings above."
    echo ""
    echo "Recommended Actions:"
    echo "1. Update config.yml with recommended values"
    echo "2. Rebuild binary: go build -o netscan ./cmd/netscan"
    echo "3. Restart service: docker compose down && docker compose up -d"
    echo "4. Monitor ping success rate for 15+ minutes"
    echo ""
    echo "For detailed guidance, see: TROUBLESHOOTING_PING_FAILURES.md"
    exit 1
else
    echo "✅ Configuration looks good!"
    echo ""
    echo "Next Steps:"
    echo "1. If using Docker: docker compose up -d"
    echo "2. Monitor logs: docker compose logs -f netscan"
    echo "3. Check health: curl http://localhost:8080/health | jq"
    echo "4. Verify ping success rate after 15+ minutes of operation"
    echo ""
    echo "For monitoring guidance, see: TROUBLESHOOTING_PING_FAILURES.md"
fi
