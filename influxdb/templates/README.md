# InfluxDB Dashboard Templates

This directory contains dashboard templates that are automatically provisioned when the InfluxDB container starts for the first time.

## Dashboard Files

### 1. `netscan.json`
**Purpose:** Primary network monitoring dashboard for the netscan service.

**Provides:**
- Real-time device discovery metrics
- Ping latency trends and statistics
- Device uptime monitoring
- Network health overview

**Source:** User-provided (manually added)

### 2. `influxdb_health.json`
**Purpose:** Application health monitoring dashboard for the netscan service.

**Provides:**
- Active pinger count
- Memory usage (heap and RSS)
- Goroutine count
- InfluxDB write success/failure rates
- Device count tracking

**Source:** User-provided (manually added)

### 3. `influxdb_operational_monitoring.yml`
**Purpose:** InfluxDB internal operational monitoring dashboard.

**Provides:**
- InfluxDB task execution metrics
- Database cardinality tracking
- Query performance metrics
- System resource utilization

**Source:** Public community template from [influxdata/community-templates](https://github.com/influxdata/community-templates/tree/master/influxdb2_operational_monitoring)

## Auto-Provisioning

All dashboards in this directory are automatically applied during the InfluxDB initialization process via the `init-influxdb.sh` script. The provisioning happens only on first startup when the database is initialized.

### How It Works

1. The `docker-compose.yml` mounts this directory as `/templates` inside the InfluxDB container (read-only)
2. The `init-influxdb.sh` script runs after InfluxDB is ready
3. Each template file is applied using the `influx apply` command
4. Dashboards become immediately available in the InfluxDB UI at http://localhost:8086

### Adding Custom Dashboards

To add additional dashboards:

1. Export your dashboard from the InfluxDB UI (Settings → Templates → Export)
2. Save the exported JSON/YAML file to this directory
3. Add an `influx apply` command to the `init-influxdb.sh` script
4. Rebuild and restart the stack: `docker compose down -v && docker compose up -d`

**Note:** The `-v` flag removes volumes, which forces re-initialization and dashboard provisioning.
