#!/usr/bin/env bash
# netscan installation and deployment script
set -e

# Variables
BINARY=netscan
CONFIG=config.yml
INSTALL_DIR=/opt/netscan
SERVICE_FILE=/etc/systemd/system/netscan.service
SERVICE_USER=netscan

# Build the binary
if [ ! -f "$BINARY" ]; then
    echo "Building netscan binary..."
    go build -o $BINARY ./cmd/netscan
fi

# Create dedicated service user
if ! id "$SERVICE_USER" &>/dev/null; then
    echo "Creating service user: $SERVICE_USER"
    sudo useradd -r -s /bin/false $SERVICE_USER
fi

# Create install directory
sudo mkdir -p $INSTALL_DIR
sudo cp $BINARY $INSTALL_DIR/
if [ -f "$CONFIG" ]; then
    sudo cp $CONFIG $INSTALL_DIR/
else
    echo "Warning: $CONFIG not found. Please copy your config file to $INSTALL_DIR manually."
fi

# Set ownership and permissions
sudo chown -R $SERVICE_USER:$SERVICE_USER $INSTALL_DIR
sudo chmod 755 $INSTALL_DIR/$BINARY

# Set capabilities for ICMP access (raw sockets)
echo "Setting CAP_NET_RAW capability for ICMP access..."
sudo setcap cap_net_raw+ep $INSTALL_DIR/$BINARY

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
User=$SERVICE_USER
Group=$SERVICE_USER

# Security hardening
NoNewPrivileges=yes
PrivateTmp=yes
ProtectSystem=strict
ReadWritePaths=$INSTALL_DIR
ProtectHome=yes

[Install]
WantedBy=multi-user.target
EOF

# Reload systemd and enable service
sudo systemctl daemon-reload
sudo systemctl enable netscan
sudo systemctl start netscan

echo "netscan deployed and running as a systemd service with dedicated user and capabilities."
