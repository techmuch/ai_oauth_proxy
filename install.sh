#!/bin/bash
set -euo pipefail

# install.sh - Installer script for Custom AI Proxy and TUI Tool

echo "=========================================="
echo "    AI OAUTH PROXY DEPLOYMENT SYSTEM     "
echo "=========================================="

# 1. Verify Superuser Privileges
if [ "$EUID" -ne 0 ]; then
  echo "Error: This script must be run as root or with sudo privileges." >&2
  exit 1
fi

# 2. Check for Go Binary availability
BINARY_NAME="ai_oauth_proxy"
BINARY_SRC="./${BINARY_NAME}"
INSTALL_DEST="/usr/local/bin/${BINARY_NAME}"

if [ ! -f "$BINARY_SRC" ]; then
  echo "Binary '${BINARY_NAME}' not found in current directory."
  if command -v go >/dev/null 2>&1; then
    echo "Go is installed. Attempting to build the binary automatically..."
    go build -o "$BINARY_NAME" .
    echo "Build completed successfully."
  else
    echo "Error: Binary not found and Go is not installed to compile it." >&2
    exit 1
  fi
fi

# 3. Copy Binary and Set Permissions
echo "Installing binary to ${INSTALL_DEST}..."
cp "$BINARY_SRC" "$INSTALL_DEST"
chmod 755 "$INSTALL_DEST"
echo "Binary installed successfully with executable permissions."

# 4. Generate systemd service unit
SERVICE_FILE="/etc/systemd/system/ai-oauth-proxy.service"
echo "Generating systemd service file at ${SERVICE_FILE}..."

cat << 'EOF' > "$SERVICE_FILE"
[Unit]
Description=Custom AI Proxy and TUI Tool Service
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/root/projects/ai_oauth_proxy
Environment=HOME=/root
Environment=PATH=/root/.local/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
ExecStart=/usr/local/bin/ai_oauth_proxy -mode=server -port=8080
Restart=always

[Install]
WantedBy=multi-user.target
EOF

# 5. Enable and Start the service
echo "Reloading systemd daemon..."
systemctl daemon-reload

echo "Enabling ai-oauth-proxy.service at boot..."
systemctl enable ai-oauth-proxy.service

echo "Starting ai-oauth-proxy.service..."
systemctl restart ai-oauth-proxy.service

# 6. Verification
echo "Verifying installation and service operation..."
sleep 2

if systemctl is-active ai-oauth-proxy.service >/dev/null 2>&1; then
  echo "Service is active and running!"
  
  # Perform local HTTP verification curl
  if command -v curl >/dev/null 2>&1; then
    RESPONSE=$(curl -s http://localhost:8080/v1/models || true)
    if echo "$RESPONSE" | grep -q "claude-sonnet-4-6"; then
      echo ""
      echo "--------------------------------------------------"
      echo "  DEPLOYMENT AND VERIFICATION SUCCESSFUL!"
      echo "  AI Proxy Server running on: http://localhost:8080"
      echo "--------------------------------------------------"
      echo ""
    else
      echo "Warning: Service is running but local endpoint validation response was unexpected: $RESPONSE"
    fi
  else
    echo "Warning: curl is not available to verify local port responsiveness."
  fi
else
  echo "Error: The service failed to start. Please check the logs with:" >&2
  echo "  journalctl -u ai-oauth-proxy.service -n 50" >&2
  exit 1
fi
