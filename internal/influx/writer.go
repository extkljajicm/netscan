package influx

import (
	"context"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
)

// Writer handles InfluxDB v2 time-series data writes
type Writer struct {
	client   influxdb2.Client     // InfluxDB client instance
	writeAPI api.WriteAPIBlocking // Blocking write API for synchronous writes
	org      string               // InfluxDB organization name
	bucket   string               // InfluxDB bucket name
}

// NewWriter creates a new InfluxDB writer with blocking write API
func NewWriter(url, token, org, bucket string) *Writer {
	client := influxdb2.NewClient(url, token)
	writeAPI := client.WriteAPIBlocking(org, bucket)
	return &Writer{
		client:   client,
		writeAPI: writeAPI,
		org:      org,
		bucket:   bucket,
	}
}

// WriteDeviceInfo writes device metadata to InfluxDB (call once per device or when SNMP data changes)
func (w *Writer) WriteDeviceInfo(ip, hostname, sysName, sysDescr, sysObjectID string) error {
	p := influxdb2.NewPointWithMeasurement("device_info")
	p.AddTag("ip", ip) // Stable identifier
	p.AddField("hostname", hostname)
	p.AddField("snmp_name", sysName)
	p.AddField("snmp_description", sysDescr)
	p.AddField("snmp_sysid", sysObjectID)
	p.SetTime(time.Now())
	return w.writeAPI.WritePoint(context.Background(), p)
}

// WritePingResult writes ICMP ping metrics to InfluxDB (optimized for time-series)
func (w *Writer) WritePingResult(ip string, rtt time.Duration, successful bool) error {
	p := influxdb2.NewPointWithMeasurement("ping")
	p.AddTag("ip", ip) // Only IP as tag for low cardinality
	p.AddField("rtt_ms", float64(rtt.Milliseconds()))
	p.AddField("success", successful)
	p.SetTime(time.Now())
	return w.writeAPI.WritePoint(context.Background(), p)
}

// Close terminates the InfluxDB client connection
func (w *Writer) Close() {
	w.client.Close()
}
