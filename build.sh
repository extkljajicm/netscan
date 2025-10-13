#!/usr/bin/env bash
# Build netscan executable only
set -e

BINARY=netscan

# Build the binary
if [ -f "$BINARY" ]; then
    echo "Removing old $BINARY..."
    rm -f $BINARY
fi

echo "Building netscan..."
go build -o $BINARY ./cmd/netscan

echo "Build complete: $BINARY"
