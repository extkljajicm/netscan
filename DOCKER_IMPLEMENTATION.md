# Docker Implementation Summary

## Overview
This document describes the Docker implementation for the netscan network monitoring application using local builds and Docker Compose for deployment.

## Files Added

### 1. Dockerfile
**Location**: `/Dockerfile`

A multi-stage Docker build configuration that:
- **Stage 1 (Builder)**: Uses `golang:1.25-alpine` to compile the Go binary
  - Installs build dependencies (git, ca-certificates)
  - Leverages Docker layer caching for go modules
  - Builds a static binary with optimizations for linux/amd64
- **Stage 2 (Runtime)**: Uses `alpine:latest` for minimal image size
  - Installs runtime dependencies (ca-certificates, libcap)
  - Creates non-root user `netscan` for security
  - Sets `CAP_NET_RAW` capability for ICMP ping access
  - Configures proper ownership and permissions
  - Exposes config path via environment variable

**Key Features**:
- Multi-stage build reduces final image size
- Linux/amd64 architecture support
- Non-root user execution for security
- Linux capabilities instead of full root access
- Optimized layer caching for faster builds

### 2. .dockerignore
**Location**: `/.dockerignore`

Excludes unnecessary files from Docker build context:
- Git files and directories
- Build artifacts and binaries
- IDE/editor files
- Documentation (except README)
- Scripts not needed in container
- Test files
- Config files (mounted at runtime)

**Benefits**:
- Faster build times
- Smaller build context
- Improved caching efficiency

### 3. GitHub Actions Workflow
**Location**: `/.github/workflows/dockerize_netscan.yml`

Automated Docker Compose testing workflow:
- **Triggers**: Push to main, pull requests, manual dispatch
- **Purpose**: Validates that the Docker Compose stack builds and runs correctly
- **Features**:
  - Creates config.yml from template
  - Builds Docker images locally
  - Starts the complete stack (netscan + InfluxDB)
  - Verifies services are running
  - Checks logs for errors
  - Cleans up after testing

### 4. Docker Compose Configuration
**Location**: `/docker-compose.yml`

Complete deployment stack including:
- **netscan service**:
  - Builds from local Dockerfile
  - Host networking for ICMP/SNMP access
  - CAP_NET_RAW capability
  - Config file mounted as read-only volume
  - Depends on InfluxDB health check
  - Health check dependency on InfluxDB
- **InfluxDB service**:
  - Persistent volume for data
  - Health check configuration
  - Pre-configured for netscan

**Usage**:
```bash
docker compose up -d
```

### 5. README Updates
**Location**: `/README.md`

Added comprehensive Docker documentation:
- **Deployment Section**: New "Docker (Recommended for Containers)" subsection
  - Quick start with Docker Compose
  - Step-by-step instructions for local deployment
  - Configuration notes and best practices
- **Service Management Section**: Added Docker Compose commands
  - View status, logs
  - Restart, rebuild services
- **Building Section**: Added Docker image build instructions
  - Local builds with Docker Compose
  - Manual Docker builds
- **Cross-Platform Builds**: Updated to reflect Docker support
  - Docker images for linux/amd64
  - Native binaries for amd64

## Docker Image Usage

### Build and Run with Docker Compose (Recommended)

```bash
# Clone the repository
git clone https://github.com/extkljajicm/netscan.git
cd netscan

# Create config file from template
cp config.yml.example config.yml

# Edit config.yml with your network settings
nano config.yml

# Build and start the stack
docker compose up -d

# View logs
docker compose logs -f netscan

# Stop the stack
docker compose down
```

### Build Locally

```bash
# Build the Docker image
docker compose build

# Or build with docker directly
docker build -t netscan:local .
```

## Security Considerations

1. **Non-root Execution**: Container runs as `netscan` user (not root)
2. **Linux Capabilities**: Only `CAP_NET_RAW` granted (for ICMP ping)
3. **Read-only Config**: Config file mounted as read-only
4. **Environment Variables**: Sensitive credentials passed via env vars
5. **Image Provenance**: Build attestation for supply chain security
6. **Minimal Base Image**: Alpine Linux for reduced attack surface

## CI/CD Integration

The workflow automatically:
1. Builds Docker images on every push to main
2. Creates multi-platform images (amd64, arm64)
3. Tags images appropriately based on git refs
4. Pushes to GitHub Container Registry
5. Generates build provenance attestation
6. Caches layers for faster builds

## Testing

### Local Testing

```bash
# Build the image
docker build -t netscan:test .

# Run with test config
docker run --rm \
  --network host \
  --cap-add=NET_RAW \
  -v $(pwd)/config.yml.example:/app/config.yml:ro \
  netscan:test --help
```

### Integration Testing

```bash
# Start full stack with docker-compose
docker-compose -f docker-compose.netscan.yml up -d

# View logs
docker-compose -f docker-compose.netscan.yml logs -f netscan

# Stop stack
docker-compose -f docker-compose.netscan.yml down
```

## Troubleshooting

### Permission Denied for ICMP
- Ensure `--cap-add=NET_RAW` is specified
- Verify container runs with correct capabilities: `docker inspect netscan`

### Network Access Issues
- Use `--network host` mode for ICMP and SNMP access
- Bridge mode doesn't allow raw socket access

### Config File Not Found
- Verify config file path is correct
- Ensure volume mount syntax is correct: `-v $(pwd)/config.yml:/app/config.yml:ro`

### Image Pull Fails
- Check if image exists: `docker pull ghcr.io/extkljajicm/netscan:latest`
- Verify internet connectivity
- For private repos, authenticate: `docker login ghcr.io`

## Future Enhancements

Potential improvements:
1. **Kubernetes Support**: Add Helm chart or Kubernetes manifests
2. **Health Checks**: Add Docker HEALTHCHECK instruction
3. **Metrics Endpoint**: Expose Prometheus metrics
4. **Configuration UI**: Web interface for configuration
5. **Multi-arch Testing**: Automated testing on ARM platforms
6. **Image Scanning**: Add vulnerability scanning to CI/CD

## References

- [Docker Multi-stage Builds](https://docs.docker.com/build/building/multi-stage/)
- [GitHub Container Registry](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry)
- [Docker Buildx](https://docs.docker.com/buildx/working-with-buildx/)
- [Linux Capabilities](https://man7.org/linux/man-pages/man7/capabilities.7.html)
