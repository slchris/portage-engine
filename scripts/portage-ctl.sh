#!/usr/bin/env bash
# Portage Engine Service Management Script
# Usage: portage-ctl {start|stop|restart|status|logs}

set -e

SERVICES=("portage-server" "portage-dashboard" "portage-builder")

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

show_status() {
    echo -e "${BLUE}=== Portage Engine Service Status ===${NC}"
    for service in "${SERVICES[@]}"; do
        echo ""
        systemctl status "${service}.service" --no-pager || true
    done
}

start_services() {
    echo -e "${GREEN}Starting Portage Engine services...${NC}"
    for service in "${SERVICES[@]}"; do
        echo "Starting ${service}..."
        systemctl start "${service}.service"
    done
    echo -e "${GREEN}All services started${NC}"
}

stop_services() {
    echo -e "${YELLOW}Stopping Portage Engine services...${NC}"
    for service in "${SERVICES[@]}"; do
        echo "Stopping ${service}..."
        systemctl stop "${service}.service"
    done
    echo -e "${YELLOW}All services stopped${NC}"
}

restart_services() {
    echo -e "${YELLOW}Restarting Portage Engine services...${NC}"
    for service in "${SERVICES[@]}"; do
        echo "Restarting ${service}..."
        systemctl restart "${service}.service"
    done
    echo -e "${GREEN}All services restarted${NC}"
}

show_logs() {
    SERVICE="${1:-portage-server}"
    echo -e "${BLUE}Showing logs for ${SERVICE}...${NC}"
    echo -e "${YELLOW}Press Ctrl+C to exit${NC}"
    journalctl -u "${SERVICE}.service" -f
}

show_urls() {
    echo ""
    echo -e "${BLUE}=== Portage Engine URLs ===${NC}"
    echo -e "${GREEN}Server:${NC}    http://$(hostname -I | awk '{print $1}'):8080"
    echo -e "${GREEN}Dashboard:${NC} http://$(hostname -I | awk '{print $1}'):3000"
    echo -e "${GREEN}Builder:${NC}   http://$(hostname -I | awk '{print $1}'):9090"
    echo ""
}

show_help() {
    cat << EOF
Portage Engine Service Management Tool

Usage: $(basename "$0") [COMMAND] [OPTIONS]

Commands:
    start           Start all services
    stop            Stop all services
    restart         Restart all services
    status          Show status of all services
    logs [SERVICE]  Show logs (default: portage-server)
    urls            Show service URLs
    help            Show this help message

Services:
    portage-server
    portage-dashboard
    portage-builder

Examples:
    $(basename "$0") start
    $(basename "$0") status
    $(basename "$0") logs portage-builder
    $(basename "$0") restart

EOF
}

# Check if running as root
if [[ $EUID -ne 0 ]]; then
    echo -e "${RED}Error: This script must be run as root${NC}"
    echo "Try: sudo $(basename "$0") $*"
    exit 1
fi

# Main command handler
case "${1:-status}" in
    start)
        start_services
        sleep 2
        show_status
        show_urls
        ;;
    stop)
        stop_services
        ;;
    restart)
        restart_services
        sleep 2
        show_status
        show_urls
        ;;
    status)
        show_status
        show_urls
        ;;
    logs)
        show_logs "${2}"
        ;;
    urls)
        show_urls
        ;;
    help|--help|-h)
        show_help
        ;;
    *)
        echo -e "${RED}Unknown command: ${1}${NC}"
        echo ""
        show_help
        exit 1
        ;;
esac
