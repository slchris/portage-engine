#!/bin/bash
# Test entry script for portage environment

set -e

# Create necessary directories
mkdir -p /var/lib/portage-engine
mkdir -p /tmp/portage-work
mkdir -p /tmp/portage-artifacts

# Set proper permissions
chmod 755 /var/lib/portage-engine
chmod 755 /tmp/portage-work
chmod 755 /tmp/portage-artifacts

# Run the tests
exec "$@"
