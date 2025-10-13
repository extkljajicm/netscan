#!/usr/bin/env bash
# netscan installation and deployment script
set -e

# Variables
BINARY=netscan
CONFIG=config.yml
INSTALL_DIR=/opt/netscan
SERVICE_FILE=/etc/systemd/system/netscan.service

# Build the binary
if [ ! -f "$BINARY" ]; then
    echo "Building netscan binary..."
    go build -o $BINARY ./cmd/netscan
fi

# Create install directory
sudo mkdir -p $INSTALL_DIR
sudo cp $BINARY $INSTALL_DIR/
if [ -f "$CONFIG" ]; then
    sudo cp $CONFIG $INSTALL_DIR/
else
    echo "Warning: $CONFIG not found. Please copy your config file to $INSTALL_DIR manually."
fi
sudo chown root:root $INSTALL_DIR/$BINARY
sudo chmod 755 $INSTALL_DIR/$BINARY

# Create systemd service
cat <<EOF | sudo tee $SERVICE_FILE
[Unit]
Description=netscan network monitoring service
After=network.target

[Service]
Type=simple
ExecStart=$INSTALL_DIR/$BINARY
WorkingDirectory=$INSTALL_DIR
Restart=always
User=root

[Install]
WantedBy=multi-user.target
EOF

# Reload systemd and enable service
sudo systemctl daemon-reload
sudo systemctl enable netscan
sudo systemctl start netscan

echo "netscan deployed and running as a systemd service."
