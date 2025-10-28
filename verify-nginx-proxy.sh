#!/bin/bash
# verify-nginx-proxy.sh - Validation script for Nginx HTTPS proxy setup

set -e

echo "=========================================="
echo "Nginx HTTPS Proxy Validation Script"
echo "=========================================="
echo

# Check if required files exist
echo "1. Checking nginx configuration files..."
required_files=(
    "nginx/nginx.conf"
    "nginx/Dockerfile"
    "nginx/docker-entrypoint.sh"
    "docker-compose.yml"
)

for file in "${required_files[@]}"; do
    if [ -f "$file" ]; then
        echo "   ✓ $file exists"
    else
        echo "   ✗ $file missing"
        exit 1
    fi
done
echo

# Validate docker-compose configuration
echo "2. Validating docker-compose.yml..."
if docker compose config --quiet; then
    echo "   ✓ docker-compose.yml is valid"
else
    echo "   ✗ docker-compose.yml validation failed"
    exit 1
fi
echo

# Check if nginx service is defined
echo "3. Checking nginx service definition..."
if docker compose config | grep -q "nginx:"; then
    echo "   ✓ nginx service is defined"
else
    echo "   ✗ nginx service not found in docker-compose.yml"
    exit 1
fi
echo

# Check if influxdb port 8086 is NOT exposed
echo "4. Verifying InfluxDB port is not exposed to host..."
if docker compose config | grep -A 10 "influxdb:" | grep -q "8086:8086"; then
    echo "   ✗ InfluxDB port 8086 is still exposed to host (should be removed)"
    exit 1
else
    echo "   ✓ InfluxDB port 8086 is not exposed to host"
fi
echo

# Check if nginx exposes ports 80 and 443
echo "5. Verifying Nginx proxy ports..."
if docker compose config | grep -A 50 "nginx:" | grep 'published:' | grep -q '"80"'; then
    echo "   ✓ Nginx exposes port 80 (HTTP)"
else
    echo "   ✗ Nginx port 80 not found"
    exit 1
fi

if docker compose config | grep -A 50 "nginx:" | grep 'published:' | grep -q '"443"'; then
    echo "   ✓ Nginx exposes port 443 (HTTPS)"
else
    echo "   ✗ Nginx port 443 not found"
    exit 1
fi
echo

# Check nginx.conf syntax (basic check)
echo "6. Validating nginx.conf syntax..."
if grep -q "proxy_pass http://influxdb:8086" nginx/nginx.conf; then
    echo "   ✓ nginx.conf contains proxy_pass to influxdb:8086"
else
    echo "   ✗ nginx.conf missing proxy_pass directive"
    exit 1
fi

if grep -q "ssl_certificate /etc/nginx/ssl/cert.pem" nginx/nginx.conf; then
    echo "   ✓ nginx.conf references SSL certificates"
else
    echo "   ✗ nginx.conf missing SSL certificate configuration"
    exit 1
fi
echo

# Check entrypoint script
echo "7. Validating docker-entrypoint.sh..."
if grep -q "openssl req -x509" nginx/docker-entrypoint.sh; then
    echo "   ✓ docker-entrypoint.sh generates SSL certificates"
else
    echo "   ✗ docker-entrypoint.sh missing certificate generation"
    exit 1
fi

if [ -x nginx/docker-entrypoint.sh ]; then
    echo "   ✓ docker-entrypoint.sh is executable"
else
    echo "   ⚠ docker-entrypoint.sh is not executable (will be set in Dockerfile)"
fi
echo

# Summary
echo "=========================================="
echo "✓ All validation checks passed!"
echo "=========================================="
echo
echo "The Nginx HTTPS proxy is configured correctly."
echo
echo "To test the full stack:"
echo "  1. docker compose up -d"
echo "  2. Wait for services to start (30-60 seconds)"
echo "  3. Access https://localhost in your browser"
echo "  4. Accept the self-signed certificate warning"
echo "  5. Login with: admin / admin123"
echo
