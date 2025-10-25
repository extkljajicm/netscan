#!/bin/bash
# InfluxDB initialization script to create the health bucket
# This script runs after the container starts and waits for InfluxDB to be ready

set -e

echo "Waiting for InfluxDB to be ready..."
until influx ping &>/dev/null; do
  echo "InfluxDB not ready yet, waiting..."
  sleep 2
done

echo "InfluxDB is ready. Checking for health bucket..."

# Check if health bucket exists
if influx bucket list --name health --org "${DOCKER_INFLUXDB_INIT_ORG}" --token "${DOCKER_INFLUXDB_INIT_ADMIN_TOKEN}" --json 2>/dev/null | grep -q '"name":"health"'; then
  echo "Health bucket already exists"
else
  echo "Creating health bucket..."
  influx bucket create \
    --name health \
    --org "${DOCKER_INFLUXDB_INIT_ORG}" \
    --token "${DOCKER_INFLUXDB_INIT_ADMIN_TOKEN}" \
    --retention "${DOCKER_INFLUXDB_INIT_RETENTION}"
  echo "Health bucket created successfully"
fi
