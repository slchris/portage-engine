#!/usr/bin/env bash
# Rollback Portage Engine deployment
# Usage: ./scripts/rollback.sh

set -e

REMOTE_HOST="${DEPLOY_HOST:-10.42.0.1}"
REMOTE_USER="${DEPLOY_USER:-chris}"
SERVICE_USER="${SERVICE_USER:-portage}"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Stop all services
stop_services() {
    log_info "Stopping services..."
    ssh "${REMOTE_USER}@${REMOTE_HOST}" "sudo systemctl stop portage-server portage-dashboard portage-builder" || true
    log_info "Services stopped"
}

# Disable services
disable_services() {
    log_info "Disabling services..."
    ssh "${REMOTE_USER}@${REMOTE_HOST}" "sudo systemctl disable portage-server portage-dashboard portage-builder" || true
    log_info "Services disabled"
}

# Remove systemd service files
remove_systemd() {
    log_info "Removing systemd service files..."
    ssh "${REMOTE_USER}@${REMOTE_HOST}" "sudo rm -f /etc/systemd/system/portage-{server,dashboard,builder}.service"
    ssh "${REMOTE_USER}@${REMOTE_HOST}" "sudo systemctl daemon-reload"
    log_info "Systemd files removed"
}

# Remove binaries (but keep data and configs)
remove_binaries() {
    log_info "Removing binaries..."
    ssh "${REMOTE_USER}@${REMOTE_HOST}" "sudo rm -rf /opt/portage-engine/bin/*"
    log_info "Binaries removed"
}

# Full cleanup (including data and configs)
full_cleanup() {
    log_warn "Performing full cleanup (including data and configs)..."
    read -p "Are you sure? This will delete ALL data! (yes/no): " confirm
    if [[ "${confirm}" != "yes" ]]; then
        log_info "Full cleanup cancelled"
        return
    fi

    ssh "${REMOTE_USER}@${REMOTE_HOST}" "sudo bash -c '
        rm -rf /opt/portage-engine
        rm -rf /etc/portage-engine
        rm -rf /var/log/portage-engine
        rm -rf /var/lib/portage-engine
        # Keep binpkg cache by default
        # rm -rf /var/cache/binpkgs
    '"
    log_warn "Full cleanup complete"
}

# Remove service user
remove_user() {
    log_warn "Removing service user..."
    read -p "Remove ${SERVICE_USER} user? (yes/no): " confirm
    if [[ "${confirm}" != "yes" ]]; then
        log_info "User removal cancelled"
        return
    fi

    ssh "${REMOTE_USER}@${REMOTE_HOST}" "sudo userdel ${SERVICE_USER}" || true
    log_info "User removed"
}

# Show menu
show_menu() {
    cat << EOF

Portage Engine Rollback Options:

1) Stop services only
2) Stop and disable services
3) Remove systemd files
4) Remove binaries (keep configs and data)
5) Full cleanup (remove everything except binpkg cache)
6) Remove service user
7) Complete uninstall (all of the above)
0) Exit

EOF
}

# Main menu loop
main() {
    while true; do
        show_menu
        read -p "Select option: " choice

        case $choice in
            1)
                stop_services
                ;;
            2)
                stop_services
                disable_services
                ;;
            3)
                stop_services
                disable_services
                remove_systemd
                ;;
            4)
                stop_services
                disable_services
                remove_systemd
                remove_binaries
                ;;
            5)
                stop_services
                disable_services
                remove_systemd
                full_cleanup
                ;;
            6)
                remove_user
                ;;
            7)
                stop_services
                disable_services
                remove_systemd
                full_cleanup
                remove_user
                log_info "Complete uninstall finished"
                break
                ;;
            0)
                log_info "Exiting"
                break
                ;;
            *)
                log_error "Invalid option"
                ;;
        esac

        echo ""
        read -p "Press Enter to continue..."
    done
}

main
