package influx

import (
	"context"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
)

type Writer struct {
	client   influxdb2.Client
	writeAPI api.WriteAPIBlocking
	org      string
	bucket   string
}

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

func (w *Writer) WritePingResult(ip, hostname string, rtt time.Duration, successful bool) error {
	p := influxdb2.NewPointWithMeasurement("ping")
	p.AddTag("ip", ip)
	p.AddTag("hostname", hostname)
	p.AddField("rtt_ms", float64(rtt.Milliseconds()))
	p.AddField("success", successful)
	p.SetTime(time.Now())
	return w.writeAPI.WritePoint(context.Background(), p)
}

func (w *Writer) Close() {
	w.client.Close()
}
