# netscan

Production-grade network monitoring service that performs automated ICMP discovery, continuous ping monitoring, and SNMP metadata collection with time-series storage in InfluxDB.

---

## Docker Quick Start

**1. Clone the Repository**
```bash
git clone https://github.com/kljama/netscan.git
cd netscan
```

**2. Create Configuration File**
```bash
cp config.yml.example config.yml
```

**3. ⚠️ CRITICAL: Edit Your Network Configuration**

> **WARNING:** You must edit `config.yml` and update the `networks:` section with your actual network ranges. The default example network will not discover devices on your network!

Open `config.yml` and modify the `networks:` section:
```yaml
networks:
  - "192.168.1.0/24"    # Replace with YOUR actual network range
  - "10.0.50.0/24"      # Add additional ranges as needed
```

Use `ip addr` (Linux) or `ipconfig` (Windows) to determine your network range.

**4. (Optional) Configure Production Credentials**

For production deployments, create a `.env` file to override default credentials:
```bash
cp .env.example .env
# Edit .env with your secure credentials
```

See [MANUAL.md](MANUAL.md#3-configure-credentials-optional-but-recommended-for-production) for details.

**5. Start the Service**
```bash
docker compose up -d
```

This starts both netscan and InfluxDB v2.7 using default credentials (suitable for testing).

---

## Verify It's Running

**View Logs:**
```bash
docker compose logs -f netscan
```

**Check Health Status:**
```bash
curl http://localhost:8080/health
```

**Access InfluxDB UI (optional):**  
Navigate to http://localhost:8086
* Username: `admin`
* Password: `admin123`
* Organization: `test-org`

---

## Stopping the Service

**Stop (keeps data):**
```bash
docker compose down
```

**Stop and delete all data:**
```bash
docker compose down -v
```

---

## Further Documentation

For detailed configuration, performance tuning, troubleshooting, and advanced deployment options, see **[MANUAL.md](MANUAL.md)**.

**Alternative Deployment:** A native systemd deployment option is available for maximum security (runs as non-root with capability-based ICMP access). See [MANUAL.md](MANUAL.md) for instructions.

---

## License

MIT License - See LICENSE.md
