#!/bin/bash
# hack/gce-start-hub.sh - Build and start Scion Hub on GCE with Caddy Reverse Proxy

set -euo pipefail

INSTANCE_NAME="scion-demo"
ZONE="us-central1-a"
PROJECT_ID=$(gcloud config get-value project 2>/dev/null)
DOMAIN="hub.demo.scion-ai.dev"
REPO_DIR="/home/scion/scion-agent"
SCION_BIN="/usr/local/bin/scion"

if [[ -z "$PROJECT_ID" ]]; then
    echo "Error: PROJECT_ID is not set and could not be determined from gcloud config."
    exit 1
fi

echo "=== Managing Scion Hub on ${INSTANCE_NAME} ==="

# Upload hub.env if it exists
if [ -f ".scratch/hub.env" ]; then
    echo "Uploading hub.env..."
    gcloud compute ssh "${INSTANCE_NAME}" --zone="${ZONE}" --command "sudo mkdir -p /home/scion/.scion && sudo chown scion:scion /home/scion/.scion"
    gcloud compute scp ".scratch/hub.env" "${INSTANCE_NAME}:/tmp/hub.env" --zone="${ZONE}"
    gcloud compute ssh "${INSTANCE_NAME}" --zone="${ZONE}" --command "sudo mv /tmp/hub.env /home/scion/.scion/hub.env && sudo chown scion:scion /home/scion/.scion/hub.env && sudo chmod 600 /home/scion/.scion/hub.env"
fi

# We use a temp file locally to avoid escaping hell on gcloud compute ssh
TMP_SERVICE=$(mktemp)
printf "[Unit]
Description=Scion Hub API Server
After=network.target nats-server.service

[Service]
User=scion
Group=scion
WorkingDirectory=%s
EnvironmentFile=/home/scion/.scion/hub.env
Environment=\"PATH=/usr/local/bin:/usr/bin:/bin\"
# Use journald for log management
StandardOutput=journal
StandardError=journal
ExecStart=%s --global server start --enable-hub --enable-runtime-host --port 9810 --runtime-host-port 9800
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
" "${REPO_DIR}" "${SCION_BIN}" > "$TMP_SERVICE"

gcloud compute scp "$TMP_SERVICE" "${INSTANCE_NAME}:/tmp/scion-hub.service" --zone="${ZONE}"
rm "$TMP_SERVICE"

# Caddyfile
TMP_CADDY=$(mktemp)
cat <<EOF > "$TMP_CADDY"
hub.demo.scion-ai.dev {
    reverse_proxy localhost:9810
    tls /etc/letsencrypt/live/demo.scion-ai.dev/fullchain.pem /etc/letsencrypt/live/demo.scion-ai.dev/privkey.pem
}
EOF
gcloud compute scp "$TMP_CADDY" "${INSTANCE_NAME}:/tmp/Caddyfile" --zone="${ZONE}"
rm "$TMP_CADDY"

gcloud compute ssh "${INSTANCE_NAME}" --zone="${ZONE}" --command '
    set -euo pipefail

    # Install Caddy
    if ! command -v caddy &>/dev/null; then
        echo "Installing Caddy..."
        sudo apt-get install -y debian-keyring debian-archive-keyring apt-transport-https curl
        curl -1sLf "https://dl.cloudsmith.io/public/caddy/stable/gpg.key" | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
        curl -1sLf "https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt" | sudo tee /etc/apt/sources.list.d/caddy-stable.list
        sudo apt-get update
        sudo apt-get install -y caddy
    fi

    # 1. Stop existing service if running
    if systemctl is-active --quiet scion-hub; then
        echo "Stopping existing scion-hub service..."
        sudo systemctl stop scion-hub
    fi

    echo "Debug: Git version info:"
    git version
    which git
    sudo -u scion git version
    sudo -u scion which git

    # 2. Update code as scion user
    echo "Updating repository..."
    sudo -u scion sh -c "cd /home/scion/scion-agent && git pull"

    # 3. Build binary as scion user from cmd/scion
    echo "Building scion binary..."
    sudo -u scion sh -c "cd /home/scion/scion-agent && /usr/local/go/bin/go build -o scion ./cmd/scion"
    
    # 4. Install binary to /usr/local/bin
    sudo mv /home/scion/scion-agent/scion /usr/local/bin/scion
    sudo chmod +x /usr/local/bin/scion

    # 5. Move systemd unit file
    echo "Updating systemd unit file..."
    sudo mv /tmp/scion-hub.service /etc/systemd/system/scion-hub.service

    echo "Debug: Service file content:"
    sudo systemctl cat scion-hub || true

    # 6. Fix Certificate Permissions for Caddy
    echo "Fixing certificate permissions for Caddy..."
    sudo chown -R root:caddy /etc/letsencrypt/live
    sudo chown -R root:caddy /etc/letsencrypt/archive
    sudo chmod -R g+rX /etc/letsencrypt/live
    sudo chmod -R g+rX /etc/letsencrypt/archive

    # 7. Configure Caddy
    echo "Updating Caddyfile..."
    sudo mv /tmp/Caddyfile /etc/caddy/Caddyfile
    sudo chown caddy:caddy /etc/caddy/Caddyfile
    sudo chmod 644 /etc/caddy/Caddyfile
    sudo systemctl restart caddy

    # 8. Start the scion-hub service
    echo "Starting scion-hub service..."
    sudo systemctl daemon-reload
    sudo systemctl enable scion-hub
    sudo systemctl start scion-hub

    # 9. Wait for service to be active
    echo "Waiting for service to start..."
    for i in {1..10}; do
        if systemctl is-active --quiet scion-hub; then
            echo "Service is active."
            break
        fi
        echo "Still waiting for service... ${i}"
        sleep 2
    done

    if ! systemctl is-active --quiet scion-hub; then
        echo "Error: Service failed to start."
        sudo journalctl -u scion-hub -n 20
        exit 1
    fi

    # 10. Local Health Check
    echo "Checking health locally on port 9810..."
    for i in {1..10}; do
        HEALTH_RESP=$(curl -s http://localhost:9810/healthz || true)
        if echo "$HEALTH_RESP" | grep -q "\"status\":\"healthy\""; then
            echo "Local health check passed: $HEALTH_RESP"
            break
        fi
        echo "Waiting for local health check... ${i}"
        echo "Response was: $HEALTH_RESP"
        sleep 2
        if [ "$i" -eq 10 ]; then
             echo "Error: Local health check failed."
             exit 1
        fi
    done
'

# 11. Remote Health check
echo "=== Performing Remote Health Check ==="
echo "Waiting for hub to be ready at https://${DOMAIN}/healthz..."
for i in {1..12}; do
    # Using -k because DNS might not be fully propagated or cert might be new
    if curl -s -k "https://${DOMAIN}/healthz" | grep -q '"status":"healthy"'; then
        echo "Hub is healthy!"
        curl -s -k "https://${DOMAIN}/healthz"
        echo ""
        exit 0
    fi
    echo "Still waiting... ($i/12)"
    sleep 5
done

echo "Error: Remote health check failed after 60 seconds."
exit 1
