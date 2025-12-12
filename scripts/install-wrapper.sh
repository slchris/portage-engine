#!/bin/bash
# Installation script for Portage Engine emerge wrapper

set -e

INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/portage"
EMERGE_ORIGINAL="/usr/bin/emerge.original"

echo "Installing Portage Engine emerge wrapper..."

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    echo "Please run as root"
    exit 1
fi

# Build the wrapper
echo "Building emerge-wrapper..."
cd "$(dirname "$0")/.."
go build -o bin/emerge-wrapper cmd/emerge-wrapper/main.go

# Backup original emerge if not already done
if [ ! -f "$EMERGE_ORIGINAL" ]; then
    echo "Backing up original emerge to $EMERGE_ORIGINAL..."
    cp /usr/bin/emerge "$EMERGE_ORIGINAL"
fi

# Install wrapper
echo "Installing wrapper to /usr/bin/emerge..."
cp bin/emerge-wrapper /usr/bin/emerge
chmod +x /usr/bin/emerge

# Create config directory
mkdir -p "$CONFIG_DIR"

# Create default config if not exists
if [ ! -f "$CONFIG_DIR/portage-engine.conf" ]; then
    echo "Creating default configuration..."
    cat > "$CONFIG_DIR/portage-engine.conf" << 'EOF'
# Portage Engine Configuration
# Enable or disable the wrapper
enabled=false

# Server URL
server_url=http://localhost:8080

# Fallback to local build if binary not available
fallback_local=true

# Timeout for server requests (seconds)
timeout_seconds=30

# Cache directory for downloaded packages
cache_dir=/var/cache/binpkgs
EOF
    chmod 644 "$CONFIG_DIR/portage-engine.conf"
fi

# Set environment variable for original emerge path
if ! grep -q "PORTAGE_ENGINE_ORIGINAL_EMERGE" /etc/environment 2>/dev/null; then
    echo "PORTAGE_ENGINE_ORIGINAL_EMERGE=$EMERGE_ORIGINAL" >> /etc/environment
fi

echo ""
echo "Installation complete!"
echo ""
echo "To enable Portage Engine:"
echo "  1. Edit /etc/portage/portage-engine.conf"
echo "  2. Set enabled=true"
echo "  3. Configure server_url if needed"
echo ""
echo "Or use environment variables:"
echo "  export PORTAGE_ENGINE_ENABLED=1"
echo "  export PORTAGE_ENGINE_SERVER=http://your-server:8080"
echo ""
echo "To uninstall:"
echo "  cp $EMERGE_ORIGINAL /usr/bin/emerge"
