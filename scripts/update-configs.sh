#!/usr/bin/env bash
# Update configuration files for deployment
# This script updates the configuration files with the correct host IP

set -e

HOST_IP="${1:-10.42.0.1}"

echo "Updating configuration files for host: ${HOST_IP}"

# Update server.conf
sed -i.bak "s|^BINPKG_PATH=.*|BINPKG_PATH=/var/cache/binpkgs|" configs/server.conf
sed -i.bak "s|^STORAGE_LOCAL_DIR=.*|STORAGE_LOCAL_DIR=/var/cache/binpkgs|" configs/server.conf

# Update builder.conf
sed -i.bak "s|^BUILD_WORK_DIR=.*|BUILD_WORK_DIR=/var/tmp/portage-builds|" configs/builder.conf
sed -i.bak "s|^BUILD_ARTIFACT_DIR=.*|BUILD_ARTIFACT_DIR=/var/tmp/portage-artifacts|" configs/builder.conf
sed -i.bak "s|^STORAGE_LOCAL_DIR=.*|STORAGE_LOCAL_DIR=/var/binpkgs|" configs/builder.conf

# Add or update SERVER_URL in builder.conf if not exists
if ! grep -q "^SERVER_URL=" configs/builder.conf; then
    echo "SERVER_URL=http://${HOST_IP}:8080" >> configs/builder.conf
else
    sed -i.bak "s|^SERVER_URL=.*|SERVER_URL=http://${HOST_IP}:8080|" configs/builder.conf
fi

# Update dashboard.conf
if ! grep -q "^SERVER_URL=" configs/dashboard.conf; then
    echo "SERVER_URL=http://${HOST_IP}:8080" >> configs/dashboard.conf
else
    sed -i.bak "s|^SERVER_URL=.*|SERVER_URL=http://${HOST_IP}:8080|" configs/dashboard.conf
fi

echo "Configuration files updated successfully"
echo ""
echo "Server:    http://${HOST_IP}:8080"
echo "Dashboard: http://${HOST_IP}:3000"
echo "Builder:   http://${HOST_IP}:9090"
