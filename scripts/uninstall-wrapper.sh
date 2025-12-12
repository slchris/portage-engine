#!/bin/bash
# Uninstallation script for Portage Engine emerge wrapper

set -e

EMERGE_ORIGINAL="/usr/bin/emerge.original"
CONFIG_FILE="/etc/portage/portage-engine.conf"

echo "Uninstalling Portage Engine emerge wrapper..."

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    echo "Please run as root"
    exit 1
fi

# Restore original emerge
if [ -f "$EMERGE_ORIGINAL" ]; then
    echo "Restoring original emerge..."
    cp "$EMERGE_ORIGINAL" /usr/bin/emerge
    rm -f "$EMERGE_ORIGINAL"
else
    echo "Warning: Original emerge backup not found at $EMERGE_ORIGINAL"
    echo "You may need to reinstall Portage manually"
fi

# Optionally remove config
read -p "Remove configuration file? (y/N) " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    rm -f "$CONFIG_FILE"
    echo "Configuration removed"
fi

# Clean environment
if [ -f /etc/environment ]; then
    sed -i '/PORTAGE_ENGINE_ORIGINAL_EMERGE/d' /etc/environment
fi

echo ""
echo "Uninstallation complete!"
