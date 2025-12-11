#!/bin/bash
# Portage Engine Client Script
# This script is installed on client systems to interface with the Portage Engine

set -e

# Configuration
PORTAGE_ENGINE_URL="${PORTAGE_ENGINE_URL:-http://localhost:8080}"
BINPKG_PATH="${BINPKG_PATH:-/var/cache/binpkgs}"
CONFIG_FILE="/etc/portage-engine/client.conf"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Load configuration if exists
if [ -f "$CONFIG_FILE" ]; then
    source "$CONFIG_FILE"
fi

# Logging functions
log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Get system architecture
get_arch() {
    uname -m
}

# Get current USE flags for a package
get_use_flags() {
    local package="$1"
    # This would parse /etc/portage/package.use or query portage
    echo "ssl xml python"
}

# Query binpkg server for package availability
query_package() {
    local package="$1"
    local version="$2"
    local arch="$(get_arch)"
    local use_flags="$(get_use_flags "$package")"

    log_info "Querying binpkg server for $package-$version ($arch)"

    response=$(curl -s -X POST \
        -H "Content-Type: application/json" \
        -d "{
            \"name\": \"$package\",
            \"version\": \"$version\",
            \"arch\": \"$arch\",
            \"use_flags\": [$(echo "$use_flags" | sed 's/ /", "/g' | sed 's/^/"/' | sed 's/$/"/')]
        }" \
        "$PORTAGE_ENGINE_URL/api/v1/packages/query")

    echo "$response"
}

# Request package build
request_build() {
    local package="$1"
    local version="$2"
    local arch="$(get_arch)"
    local use_flags="$(get_use_flags "$package")"
    local cloud_provider="${CLOUD_PROVIDER:-gcp}"

    log_info "Requesting build for $package-$version ($arch)"

    response=$(curl -s -X POST \
        -H "Content-Type: application/json" \
        -d "{
            \"package_name\": \"$package\",
            \"version\": \"$version\",
            \"arch\": \"$arch\",
            \"use_flags\": [$(echo "$use_flags" | sed 's/ /", "/g' | sed 's/^/"/' | sed 's/$/"/')],
            \"cloud_provider\": \"$cloud_provider\",
            \"machine_spec\": {
                \"region\": \"${CLOUD_REGION:-us-central1}\",
                \"zone\": \"${CLOUD_ZONE:-us-central1-a}\"
            }
        }" \
        "$PORTAGE_ENGINE_URL/api/v1/packages/request-build")

    echo "$response"
}

# Check build status
check_build_status() {
    local job_id="$1"

    response=$(curl -s "$PORTAGE_ENGINE_URL/api/v1/packages/status?job_id=$job_id")
    echo "$response"
}

# Wait for build completion
wait_for_build() {
    local job_id="$1"
    local max_wait="${2:-3600}" # Default 1 hour timeout
    local elapsed=0

    log_info "Waiting for build $job_id to complete..."

    while [ $elapsed -lt $max_wait ]; do
        status=$(check_build_status "$job_id")
        build_status=$(echo "$status" | grep -o '"status":"[^"]*"' | cut -d'"' -f4)

        case "$build_status" in
            "completed")
                log_info "Build completed successfully"
                return 0
                ;;
            "failed")
                log_error "Build failed"
                echo "$status"
                return 1
                ;;
            *)
                echo -n "."
                sleep 10
                elapsed=$((elapsed + 10))
                ;;
        esac
    done

    log_error "Build timeout after ${max_wait} seconds"
    return 1
}

# Install package
install_package() {
    local package="$1"
    local version="$2"

    # First, query for existing binpkg
    query_result=$(query_package "$package" "$version")
    found=$(echo "$query_result" | grep -o '"found":[^,]*' | cut -d':' -f2)

    if [ "$found" = "true" ]; then
        log_info "Package found in binpkg server, installing..."
        emerge --usepkg --getbinpkg "=$package-$version"
        return $?
    fi

    # Package not found, request build
    log_warn "Package not found in binpkg server, requesting build..."
    build_result=$(request_build "$package" "$version")
    job_id=$(echo "$build_result" | grep -o '"job_id":"[^"]*"' | cut -d'"' -f4)

    if [ -z "$job_id" ]; then
        log_error "Failed to request build"
        return 1
    fi

    log_info "Build requested with job ID: $job_id"

    # Wait for build to complete
    if ! wait_for_build "$job_id"; then
        return 1
    fi

    # Install the newly built package
    log_info "Installing built package..."
    emerge --usepkg --getbinpkg "=$package-$version"
}

# Configure portage to use binpkg server
configure_portage() {
    log_info "Configuring portage to use Portage Engine..."

    mkdir -p /etc/portage/binrepos.conf

    cat > /etc/portage/binrepos.conf/portage-engine.conf <<EOF
[portage-engine]
priority = 1
sync-uri = $PORTAGE_ENGINE_URL/binpkgs
EOF

    log_info "Portage configured successfully"
}

# Main command handler
case "${1:-}" in
    install)
        if [ -z "${2:-}" ]; then
            log_error "Usage: $0 install <package> [version]"
            exit 1
        fi
        install_package "$2" "${3:-}"
        ;;
    query)
        if [ -z "${2:-}" ]; then
            log_error "Usage: $0 query <package> [version]"
            exit 1
        fi
        query_package "$2" "${3:-}"
        ;;
    build)
        if [ -z "${2:-}" ]; then
            log_error "Usage: $0 build <package> [version]"
            exit 1
        fi
        request_build "$2" "${3:-}"
        ;;
    status)
        if [ -z "${2:-}" ]; then
            log_error "Usage: $0 status <job_id>"
            exit 1
        fi
        check_build_status "$2"
        ;;
    configure)
        configure_portage
        ;;
    *)
        echo "Portage Engine Client"
        echo ""
        echo "Usage: $0 <command> [arguments]"
        echo ""
        echo "Commands:"
        echo "  install <package> [version]  - Install package (query/build/install)"
        echo "  query <package> [version]    - Query binpkg availability"
        echo "  build <package> [version]    - Request package build"
        echo "  status <job_id>              - Check build status"
        echo "  configure                    - Configure portage integration"
        echo ""
        exit 1
        ;;
esac
