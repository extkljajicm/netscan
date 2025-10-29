package monitoring

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gosnmp/gosnmp"
	"github.com/kljama/netscan/internal/config"
	"github.com/kljama/netscan/internal/state"
	"github.com/rs/zerolog/log"
	"golang.org/x/time/rate"
)

// SNMPStateManager interface for updating device SNMP data and circuit breaker state
type SNMPStateManager interface {
	UpdateDeviceSNMP(ip, hostname, sysDescr string)
	ReportSNMPSuccess(ip string)
	ReportSNMPFail(ip string, maxFails int, backoff time.Duration) bool
	IsSNMPSuspended(ip string) bool
}

// SNMPWriter interface for writing device info to external storage
type SNMPWriter interface {
	WriteDeviceInfo(ip, hostname, sysDescr string) error
}

// StartSNMPPoller runs continuous SNMP polling for a single device
// This mirrors the StartPinger architecture with rate limiting and circuit breaker
func StartSNMPPoller(ctx context.Context, wg *sync.WaitGroup, device state.Device, interval time.Duration, snmpConfig *config.SNMPConfig, writer SNMPWriter, stateMgr SNMPStateManager, limiter *rate.Limiter, inFlightCounter *atomic.Int64, totalSNMPQueries *atomic.Uint64, maxConsecutiveFails int, backoffDuration time.Duration) {
	// Panic recovery for SNMP poller goroutine
	defer func() {
		if r := recover(); r != nil {
			log.Error().
				Str("ip", device.IP).
				Interface("panic", r).
				Msg("SNMP poller panic recovered")
		}
	}()

	if wg != nil {
		defer wg.Done()
	}
	
	// Initialize timer for first SNMP query with 5 second delay to avoid immediate query storm
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	
	for {
		select {
		case <-ctx.Done():
			// Stop timer on graceful shutdown
			timer.Stop()
			return
		case <-timer.C:
			// 1. CHECK CIRCUIT BREAKER *BEFORE* ACQUIRING TOKEN
			if stateMgr.IsSNMPSuspended(device.IP) {
				log.Debug().Str("ip", device.IP).Msg("SNMP polling is suspended (circuit breaker), skipping.")
				timer.Reset(interval) // Reset timer and wait for next cycle
				continue              // Skip SNMP query entirely
			}

			// 2. Acquire token from rate limiter (blocks until available or context cancelled)
			if err := limiter.Wait(ctx); err != nil {
				// Context was cancelled while waiting for token
				return
			}

			// 3. Perform the SNMP query with in-flight tracking and circuit breaker
			performSNMPQueryWithCircuitBreaker(device, snmpConfig, writer, stateMgr, inFlightCounter, totalSNMPQueries, maxConsecutiveFails, backoffDuration)
			
			// 4. Reset timer to schedule next SNMP query after interval
			// This ensures interval is time BETWEEN queries, not fixed schedule
			timer.Reset(interval)
		}
	}
}

// performSNMPQueryWithCircuitBreaker executes a single SNMP query with circuit breaker integration
func performSNMPQueryWithCircuitBreaker(device state.Device, snmpConfig *config.SNMPConfig, writer SNMPWriter, stateMgr SNMPStateManager, inFlightCounter *atomic.Int64, totalSNMPQueries *atomic.Uint64, maxConsecutiveFails int, backoffDuration time.Duration) {
	// Increment in-flight counter
	if inFlightCounter != nil {
		inFlightCounter.Add(1)
		// Ensure counter is decremented when SNMP operation completes
		defer inFlightCounter.Add(-1)
	}
	
	// Increment total SNMP queries counter (for observability)
	if totalSNMPQueries != nil {
		totalSNMPQueries.Add(1)
	}

	log.Debug().Str("ip", device.IP).Msg("Querying SNMP device")

	// Configure SNMP connection parameters
	params := &gosnmp.GoSNMP{
		Target:    device.IP,
		Port:      uint16(snmpConfig.Port),
		Community: snmpConfig.Community,
		Version:   gosnmp.Version2c,
		Timeout:   snmpConfig.Timeout,
		Retries:   snmpConfig.Retries,
	}
	
	if err := params.Connect(); err != nil {
		log.Debug().
			Str("ip", device.IP).
			Err(err).
			Msg("SNMP connection failed")
		
		// Report failure to circuit breaker
		if stateMgr != nil {
			wasSuspended := stateMgr.ReportSNMPFail(device.IP, maxConsecutiveFails, backoffDuration)
			if wasSuspended {
				log.Warn().
					Str("ip", device.IP).
					Dur("backoff", backoffDuration).
					Msg("SNMP polling failed max attempts, suspending SNMP (circuit breaker tripped)")
			}
		}
		return
	}
	defer params.Conn.Close()

	// Query standard MIB-II system OIDs: sysName, sysDescr
	// Using snmpGetWithFallback to handle devices that don't support .0 instance
	oids := []string{"1.3.6.1.2.1.1.5.0", "1.3.6.1.2.1.1.1.0"}
	resp, err := snmpGetWithFallback(params, oids)
	if err != nil || len(resp.Variables) < 2 {
		log.Debug().
			Str("ip", device.IP).
			Err(err).
			Msg("SNMP query failed")
		
		// Report failure to circuit breaker
		if stateMgr != nil {
			wasSuspended := stateMgr.ReportSNMPFail(device.IP, maxConsecutiveFails, backoffDuration)
			if wasSuspended {
				log.Warn().
					Str("ip", device.IP).
					Dur("backoff", backoffDuration).
					Msg("SNMP polling failed max attempts, suspending SNMP (circuit breaker tripped)")
			}
		}
		return
	}

	// Validate and sanitize SNMP response data
	hostname, err := validateSNMPString(resp.Variables[0].Value, "sysName")
	if err != nil {
		log.Debug().
			Str("ip", device.IP).
			Err(err).
			Msg("Invalid sysName")
		
		// Report failure to circuit breaker
		if stateMgr != nil {
			wasSuspended := stateMgr.ReportSNMPFail(device.IP, maxConsecutiveFails, backoffDuration)
			if wasSuspended {
				log.Warn().
					Str("ip", device.IP).
					Dur("backoff", backoffDuration).
					Msg("SNMP polling failed max attempts, suspending SNMP (circuit breaker tripped)")
			}
		}
		return
	}
	
	sysDescr, err := validateSNMPString(resp.Variables[1].Value, "sysDescr")
	if err != nil {
		log.Debug().
			Str("ip", device.IP).
			Err(err).
			Msg("Invalid sysDescr")
		
		// Report failure to circuit breaker
		if stateMgr != nil {
			wasSuspended := stateMgr.ReportSNMPFail(device.IP, maxConsecutiveFails, backoffDuration)
			if wasSuspended {
				log.Warn().
					Str("ip", device.IP).
					Dur("backoff", backoffDuration).
					Msg("SNMP polling failed max attempts, suspending SNMP (circuit breaker tripped)")
			}
		}
		return
	}

	// SNMP query successful
	log.Debug().
		Str("ip", device.IP).
		Str("hostname", hostname).
		Msg("SNMP query successful")
	
	// Report success to circuit breaker (resets failure count)
	if stateMgr != nil {
		stateMgr.ReportSNMPSuccess(device.IP)
		stateMgr.UpdateDeviceSNMP(device.IP, hostname, sysDescr)
	}
	
	// Write device info to InfluxDB
	if err := writer.WriteDeviceInfo(device.IP, hostname, sysDescr); err != nil {
		log.Error().
			Str("ip", device.IP).
			Err(err).
			Msg("Failed to write device info")
	}
}

// snmpGetWithFallback attempts to get SNMP OIDs using Get, falling back to GetNext if Get fails
// This is a local copy of the discovery.snmpGetWithFallback function to avoid circular imports
func snmpGetWithFallback(params *gosnmp.GoSNMP, oids []string) (*gosnmp.SnmpPacket, error) {
	// Try Get first (most efficient for .0 instances)
	resp, err := params.Get(oids)
	if err == nil {
		// Check if we got valid responses (no NoSuchInstance errors)
		hasValidData := false
		for _, variable := range resp.Variables {
			if variable.Type != gosnmp.NoSuchInstance && variable.Type != gosnmp.NoSuchObject {
				hasValidData = true
				break
			}
		}
		if hasValidData {
			return resp, nil
		}
		// All variables returned NoSuchInstance/NoSuchObject, try GetNext
		log.Debug().
			Str("target", params.Target).
			Msg("Get returned NoSuchInstance, trying GetNext fallback")
	}

	// Fallback to GetNext for each OID (works when .0 instance doesn't exist)
	baseOIDs := make([]string, len(oids))
	for i, oid := range oids {
		// Remove the .0 suffix if present to get base OID
		if len(oid) > 2 && oid[len(oid)-2:] == ".0" {
			baseOIDs[i] = oid[:len(oid)-2]
		} else {
			baseOIDs[i] = oid
		}
	}

	variables := make([]gosnmp.SnmpPDU, 0, len(baseOIDs))
	for _, baseOID := range baseOIDs {
		resp, err := params.GetNext([]string{baseOID})
		if err != nil {
			continue
		}
		if len(resp.Variables) > 0 {
			// Verify the returned OID is under the requested base OID
			returnedOID := resp.Variables[0].Name
			if len(returnedOID) >= len(baseOID) && returnedOID[:len(baseOID)] == baseOID {
				variables = append(variables, resp.Variables[0])
			}
		}
	}

	if len(variables) == 0 {
		return nil, fmt.Errorf("no valid SNMP data retrieved")
	}

	// Construct a response packet with the collected variables
	return &gosnmp.SnmpPacket{
		Variables: variables,
	}, nil
}

// validateSNMPString validates and sanitizes SNMP string values
// This is a local copy of the discovery.validateSNMPString function to avoid circular imports
func validateSNMPString(value interface{}, oidName string) (string, error) {
	var str string
	switch v := value.(type) {
	case string:
		str = v
	case []byte:
		str = string(v)
	default:
		return "", fmt.Errorf("invalid type for %s: expected string or []byte, got %T", oidName, value)
	}

	// Security: reject strings containing null bytes
	for i := 0; i < len(str); i++ {
		if str[i] == 0 {
			return "", fmt.Errorf("%s contains null byte at position %d", oidName, i)
		}
	}

	// Limit string length to prevent memory exhaustion
	if len(str) > 1024 {
		str = str[:1024] + "..."
	}

	// Sanitize: replace newlines and tabs with spaces, remove other non-printable chars
	sanitized := make([]byte, 0, len(str))
	for i := 0; i < len(str); i++ {
		ch := str[i]
		if ch == '\n' || ch == '\r' || ch == '\t' {
			sanitized = append(sanitized, ' ')
		} else if ch >= 32 && ch <= 126 {
			sanitized = append(sanitized, ch)
		}
		// Skip other non-printable characters
	}
	
	result := string(sanitized)
	
	// Trim whitespace
	result = trimSpace(result)
	
	if len(result) == 0 {
		return "", fmt.Errorf("%s is empty after sanitization", oidName)
	}

	return result, nil
}

// trimSpace removes leading and trailing whitespace
func trimSpace(s string) string {
	start := 0
	end := len(s)
	
	// Trim leading spaces
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	
	// Trim trailing spaces
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	
	return s[start:end]
}
