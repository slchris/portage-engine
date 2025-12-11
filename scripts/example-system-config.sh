#!/bin/bash
# Example script showing how to use the portage-client with system configuration

set -e

# Color output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}Portage Engine - System Configuration Example${NC}"
echo ""

# Check if /etc/portage exists
if [ ! -d "/etc/portage" ]; then
    echo "Warning: /etc/portage not found. This example requires a Gentoo system."
    echo "Creating example configuration for demonstration..."
    PORTAGE_DIR="./example-portage"
    mkdir -p "$PORTAGE_DIR"

    # Create example make.conf
    cat > "$PORTAGE_DIR/make.conf" <<EOF
# Example make.conf
CFLAGS="-O2 -pipe -march=native"
MAKEOPTS="-j\$(nproc)"
USE="ssl threads sqlite readline ncurses"
ACCEPT_LICENSE="*"
EOF

    # Create example package.use
    mkdir -p "$PORTAGE_DIR/package.use"
    cat > "$PORTAGE_DIR/package.use/custom" <<EOF
# Python with extra features
dev-lang/python ssl threads sqlite readline
dev-lang/python:3.11 -test -doc

# System packages
sys-apps/systemd -audit
app-editors/vim python vim-pager
EOF

    # Create example package.accept_keywords
    mkdir -p "$PORTAGE_DIR/package.accept_keywords"
    cat > "$PORTAGE_DIR/package.accept_keywords/testing" <<EOF
# Testing packages
dev-lang/rust ~amd64
sys-devel/gcc ~amd64
EOF

else
    PORTAGE_DIR="/etc/portage"
fi

echo -e "${GREEN}Using Portage directory: $PORTAGE_DIR${NC}"
echo ""

# Example 1: Read system config and display summary
echo -e "${BLUE}Example 1: Generate configuration bundle from system${NC}"
echo "Command: portage-client -portage-dir=$PORTAGE_DIR -package=dev-lang/python:3.11 -output=python-bundle.tar.gz"
echo ""

# Example 2: Submit build using system configuration
echo -e "${BLUE}Example 2: Submit build using system Portage configuration${NC}"
echo "Command: portage-client -portage-dir=$PORTAGE_DIR -package=dev-lang/python:3.11 -server=http://build-server:8080"
echo ""
echo "This will:"
echo "  1. Read all configuration from $PORTAGE_DIR"
echo "  2. Include package.use, package.accept_keywords, make.conf, etc."
echo "  3. Bundle with the package specification"
echo "  4. Submit to the build server"
echo ""

echo -e "${GREEN}Setup complete!${NC}"
echo ""
echo "To actually run these examples, execute the commands shown above."
