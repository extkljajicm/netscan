#!/usr/bin/env bash
# Docker setup verification script for netscan
set -e

echo "=== netscan Docker Setup Verification ==="
echo ""

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Check if Docker is installed
echo -n "Checking Docker installation... "
if command -v docker &> /dev/null; then
    echo -e "${GREEN}OK${NC}"
    docker --version
else
    echo -e "${RED}FAILED${NC}"
    echo "Docker is not installed. Please install Docker first."
    exit 1
fi

echo ""

# Check if Docker Compose is installed
echo -n "Checking Docker Compose installation... "
if command -v docker-compose &> /dev/null; then
    echo -e "${GREEN}OK${NC}"
    docker-compose --version
elif docker compose version &> /dev/null; then
    echo -e "${GREEN}OK (Docker Compose v2)${NC}"
    docker compose version
else
    echo -e "${RED}FAILED${NC}"
    echo "Docker Compose is not installed. Please install Docker Compose first."
    exit 1
fi

echo ""

# Check if Dockerfile exists
echo -n "Checking Dockerfile... "
if [ -f "Dockerfile" ]; then
    echo -e "${GREEN}OK${NC}"
else
    echo -e "${RED}FAILED${NC}"
    echo "Dockerfile not found in current directory"
    exit 1
fi

echo ""

# Check if docker-compose.yml exists
echo -n "Checking docker-compose.yml... "
if [ -f "docker-compose.yml" ]; then
    echo -e "${GREEN}OK${NC}"
else
    echo -e "${RED}FAILED${NC}"
    echo "docker-compose.yml not found in current directory"
    exit 1
fi

echo ""

# Check if config.yml.example exists
echo -n "Checking config.yml.example... "
if [ -f "config.yml.example" ]; then
    echo -e "${GREEN}OK${NC}"
else
    echo -e "${YELLOW}WARNING${NC}"
    echo "config.yml.example not found. You'll need a config file to run netscan."
fi

echo ""

# Check if config.yml exists
echo -n "Checking config.yml... "
if [ -f "config.yml" ]; then
    echo -e "${GREEN}OK${NC}"
else
    echo -e "${YELLOW}WARNING${NC}"
    echo "config.yml not found. Creating from template..."
    if [ -f "config.yml.example" ]; then
        cp config.yml.example config.yml
        echo -e "${GREEN}Created config.yml from template${NC}"
        echo "Please edit config.yml with your network settings before running."
    fi
fi

echo ""

# Validate Dockerfile syntax
echo -n "Validating Dockerfile syntax... "
if docker build --help &> /dev/null; then
    echo -e "${GREEN}OK${NC}"
else
    echo -e "${RED}FAILED${NC}"
    echo "Docker build command failed"
    exit 1
fi

echo ""

# Validate docker-compose.yml syntax
echo -n "Validating docker-compose.yml syntax... "
if docker-compose -f docker-compose.yml config > /dev/null 2>&1; then
    echo -e "${GREEN}OK${NC}"
elif docker compose -f docker-compose.yml config > /dev/null 2>&1; then
    echo -e "${GREEN}OK (Docker Compose v2)${NC}"
else
    echo -e "${RED}FAILED${NC}"
    echo "docker-compose.yml has syntax errors"
    exit 1
fi

echo ""
echo "=== Verification Complete ==="
echo ""
echo "Next steps:"
echo "1. Edit config.yml with your network settings"
echo "2. Start the stack: docker compose up -d"
echo "3. View logs: docker compose logs -f netscan"
echo "4. Stop the stack: docker compose down"
echo ""
echo "For more information, see DOCKER_IMPLEMENTATION.md"
