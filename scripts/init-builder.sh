#!/bin/bash
# Portage Builder Initialization Script for Debian 12
# This script sets up a builder instance on a fresh Debian 12 system

set -e

echo "==> Portage Builder Initialization Script"
echo "==> System: Debian 12"
echo ""

# Check if running as root
if [ "$EUID" -ne 0 ]; then
  echo "ERROR: This script must be run as root"
  exit 1
fi

# Configuration
BUILDER_PORT="${BUILDER_PORT:-9090}"
BUILDER_WORKERS="${BUILDER_WORKERS:-2}"
USE_DOCKER="${USE_DOCKER:-true}"
DOCKER_IMAGE="${DOCKER_IMAGE:-gentoo/stage3:latest}"
GPG_ENABLED="${GPG_ENABLED:-false}"

echo "==> Configuration:"
echo "    Port: $BUILDER_PORT"
echo "    Workers: $BUILDER_WORKERS"
echo "    Use Docker: $USE_DOCKER"
echo "    Docker Image: $DOCKER_IMAGE"
echo "    GPG Enabled: $GPG_ENABLED"
echo ""

# Update system
echo "==> Updating system packages..."
apt-get update
apt-get upgrade -y

# Install dependencies
echo "==> Installing dependencies..."
apt-get install -y \
    curl \
    wget \
    git \
    build-essential \
    ca-certificates \
    gnupg \
    lsb-release

# Install Docker if needed
if [ "$USE_DOCKER" = "true" ]; then
    echo "==> Installing Docker..."

    # Add Docker's official GPG key
    install -m 0755 -d /etc/apt/keyrings
    curl -fsSL https://download.docker.com/linux/debian/gpg | gpg --dearmor -o /etc/apt/keyrings/docker.gpg
    chmod a+r /etc/apt/keyrings/docker.gpg

    # Add Docker repository
    echo \
      "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/debian \
      $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | \
      tee /etc/apt/sources.list.d/docker.list > /dev/null

    # Install Docker Engine
    apt-get update
    apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

    # Enable and start Docker
    systemctl enable docker
    systemctl start docker

    # Pull Gentoo image
    echo "==> Pulling Docker image: $DOCKER_IMAGE..."
    docker pull "$DOCKER_IMAGE"

    echo "==> Docker installed successfully"
fi

# Configure firewall
echo "==> Configuring firewall..."
apt-get install -y ufw

# Allow SSH
ufw allow 22/tcp

# Allow builder port
ufw allow "$BUILDER_PORT/tcp"

# Enable firewall
echo "y" | ufw enable

echo "==> Firewall configured"

# Create builder user
echo "==> Creating builder user..."
if ! id "portage-builder" &>/dev/null; then
    useradd -r -m -s /bin/bash portage-builder

    # Add to docker group if using Docker
    if [ "$USE_DOCKER" = "true" ]; then
        usermod -aG docker portage-builder
    fi
fi

# Create directories
echo "==> Creating directories..."
mkdir -p /var/tmp/portage-builds
mkdir -p /var/tmp/portage-artifacts
mkdir -p /opt/portage-builder
mkdir -p /etc/portage-builder

chown -R portage-builder:portage-builder /var/tmp/portage-builds
chown -R portage-builder:portage-builder /var/tmp/portage-artifacts
chown -R portage-builder:portage-builder /opt/portage-builder

# Set permissions
chmod 755 /var/tmp/portage-builds
chmod 755 /var/tmp/portage-artifacts

# Download builder binary (placeholder - adjust URL as needed)
echo "==> Downloading builder binary..."
BUILDER_URL="${BUILDER_URL:-https://example.com/portage-builder}"
if [ -n "$BUILDER_BINARY_PATH" ]; then
    echo "    Using local binary: $BUILDER_BINARY_PATH"
    cp "$BUILDER_BINARY_PATH" /opt/portage-builder/portage-builder
else
    echo "    Note: Set BUILDER_BINARY_PATH to copy your pre-built binary"
    echo "    Or set BUILDER_URL to download from a URL"
fi

if [ -f /opt/portage-builder/portage-builder ]; then
    chmod +x /opt/portage-builder/portage-builder
    chown portage-builder:portage-builder /opt/portage-builder/portage-builder
fi

# Create systemd service
echo "==> Creating systemd service..."
cat > /etc/systemd/system/portage-builder.service << SERVICEEOF
[Unit]
Description=Portage Builder Service
After=network.target docker.service
Requires=docker.service

[Service]
Type=simple
User=portage-builder
Group=portage-builder
WorkingDirectory=/opt/portage-builder
ExecStart=/opt/portage-builder/portage-builder
Environment="BUILDER_PORT=$BUILDER_PORT"
Environment="WORKERS=$BUILDER_WORKERS"
Environment="USE_DOCKER=$USE_DOCKER"
Environment="DOCKER_IMAGE=$DOCKER_IMAGE"
Environment="GPG_ENABLED=$GPG_ENABLED"
Environment="BUILD_WORK_DIR=/var/tmp/portage-builds"
Environment="BUILD_ARTIFACT_DIR=/var/tmp/portage-artifacts"
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
SERVICEEOF

# Reload systemd
systemctl daemon-reload

echo ""
echo "==> Builder initialization complete!"
echo ""
echo "Next steps:"
echo "  1. Copy your builder binary to /opt/portage-builder/portage-builder"
echo "  2. Start the service: systemctl start portage-builder"
echo "  3. Enable on boot: systemctl enable portage-builder"
echo "  4. Check status: systemctl status portage-builder"
echo "  5. View logs: journalctl -u portage-builder -f"
echo ""
echo "Builder will listen on port: $BUILDER_PORT"
echo "Firewall rules have been configured to allow access"
echo ""
