#!/bin/bash

# Multi-Domain AMTP Gateway Deployment Script
# This script demonstrates how to deploy multiple gateway instances,
# each managing a different domain.

set -euo pipefail

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
BINARY_PATH="$PROJECT_ROOT/build/agentry"
CONFIG_DIR="$PROJECT_ROOT/config"
LOGS_DIR="$PROJECT_ROOT/logs"

# Domain configurations
declare -A DOMAINS=(
    ["company-a.com"]="8443"
    ["subsidiary.com"]="8444"
    ["partner.com"]="8445"
)

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Logging functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check prerequisites
check_prerequisites() {
    log_info "Checking prerequisites..."
    
    # Check if binary exists
    if [[ ! -f "$BINARY_PATH" ]]; then
        log_error "Gateway binary not found at $BINARY_PATH"
        log_info "Run 'make build' to build the binary"
        exit 1
    fi
    
    # Create logs directory
    mkdir -p "$LOGS_DIR"
    
    # Check if ports are available
    for domain in "${!DOMAINS[@]}"; do
        port="${DOMAINS[$domain]}"
        if lsof -Pi :$port -sTCP:LISTEN -t >/dev/null 2>&1; then
            log_error "Port $port is already in use (needed for $domain)"
            exit 1
        fi
    done
    
    log_success "Prerequisites check passed"
}

# Generate configuration for a domain
generate_config() {
    local domain="$1"
    local port="$2"
    local config_file="$CONFIG_DIR/config-$domain.yaml"
    
    log_info "Generating configuration for $domain on port $port"
    
    cat > "$config_file" << EOF
# AMTP Gateway Configuration for $domain
version: "1.0"

server:
  address: ":$port"
  domain: "$domain"
  read_timeout: "30s"
  write_timeout: "30s"
  idle_timeout: "120s"

tls:
  enabled: false  # Set to true in production with proper certificates

dns:
  cache_ttl: "5m"
  timeout: "5s"
  mock_mode: true  # Set to false in production
  allow_http: true  # Set to false in production

message:
  max_size: 10485760  # 10MB
  idempotency_ttl: "168h"  # 7 days
  validation_enabled: true

auth:
  require_auth: false  # Set to true in production

logging:
  level: "info"
  format: "json"
EOF
    
    log_success "Configuration generated: $config_file"
}

# Start gateway for a domain
start_gateway() {
    local domain="$1"
    local port="$2"
    local config_file="$CONFIG_DIR/config-$domain.yaml"
    local log_file="$LOGS_DIR/$domain.log"
    local pid_file="$LOGS_DIR/$domain.pid"
    
    log_info "Starting gateway for $domain on port $port"
    
    # Set environment variables
    export AMTP_DOMAIN="$domain"
    export AMTP_SERVER_ADDRESS=":$port"
    export AMTP_TLS_ENABLED=false
    export AMTP_DNS_MOCK_MODE=true
    export AMTP_DNS_ALLOW_HTTP=true
    export AMTP_AUTH_REQUIRED=false
    export AMTP_LOGGING_LEVEL=info
    export AMTP_LOGGING_FORMAT=json
    
    # Start the gateway in background
    nohup "$BINARY_PATH" -config "$config_file" > "$log_file" 2>&1 &
    local pid=$!
    
    # Save PID
    echo $pid > "$pid_file"
    
    # Wait a moment and check if process is still running
    sleep 2
    if ! kill -0 $pid 2>/dev/null; then
        log_error "Failed to start gateway for $domain"
        log_error "Check log file: $log_file"
        return 1
    fi
    
    log_success "Gateway for $domain started (PID: $pid, Port: $port)"
    log_info "Log file: $log_file"
}

# Stop gateway for a domain
stop_gateway() {
    local domain="$1"
    local pid_file="$LOGS_DIR/$domain.pid"
    
    if [[ -f "$pid_file" ]]; then
        local pid=$(cat "$pid_file")
        if kill -0 $pid 2>/dev/null; then
            log_info "Stopping gateway for $domain (PID: $pid)"
            kill $pid
            
            # Wait for graceful shutdown
            local count=0
            while kill -0 $pid 2>/dev/null && [[ $count -lt 30 ]]; do
                sleep 1
                ((count++))
            done
            
            # Force kill if still running
            if kill -0 $pid 2>/dev/null; then
                log_warning "Force killing gateway for $domain"
                kill -9 $pid
            fi
            
            log_success "Gateway for $domain stopped"
        else
            log_warning "Gateway for $domain is not running"
        fi
        rm -f "$pid_file"
    else
        log_warning "No PID file found for $domain"
    fi
}

# Stop all gateways
stop_all() {
    log_info "Stopping all gateways..."
    
    for domain in "${!DOMAINS[@]}"; do
        stop_gateway "$domain"
    done
    
    log_success "All gateways stopped"
}

# Start all gateways
start_all() {
    log_info "Starting all gateways..."
    
    for domain in "${!DOMAINS[@]}"; do
        port="${DOMAINS[$domain]}"
        generate_config "$domain" "$port"
        start_gateway "$domain" "$port"
    done
    
    log_success "All gateways started"
}

# Show status of all gateways
show_status() {
    log_info "Gateway Status:"
    echo
    
    for domain in "${!DOMAINS[@]}"; do
        port="${DOMAINS[$domain]}"
        pid_file="$LOGS_DIR/$domain.pid"
        
        if [[ -f "$pid_file" ]]; then
            pid=$(cat "$pid_file")
            if kill -0 $pid 2>/dev/null; then
                echo -e "  ${GREEN}●${NC} $domain (PID: $pid, Port: $port) - Running"
            else
                echo -e "  ${RED}●${NC} $domain - Stopped (stale PID file)"
                rm -f "$pid_file"
            fi
        else
            echo -e "  ${RED}●${NC} $domain - Stopped"
        fi
    done
    
    echo
}

# Test gateways
test_gateways() {
    log_info "Testing gateways..."
    
    for domain in "${!DOMAINS[@]}"; do
        port="${DOMAINS[$domain]}"
        
        log_info "Testing $domain on port $port"
        
        # Test health endpoint
        if curl -f -s "http://localhost:$port/health" >/dev/null; then
            log_success "$domain health check passed"
        else
            log_error "$domain health check failed"
        fi
        
        # Test capabilities endpoint
        if curl -f -s "http://localhost:$port/v1/capabilities/$domain" >/dev/null; then
            log_success "$domain capabilities check passed"
        else
            log_error "$domain capabilities check failed"
        fi
    done
}

# Show logs for a domain
show_logs() {
    local domain="$1"
    local log_file="$LOGS_DIR/$domain.log"
    
    if [[ -f "$log_file" ]]; then
        log_info "Showing logs for $domain:"
        tail -f "$log_file"
    else
        log_error "Log file not found for $domain: $log_file"
    fi
}

# Register test agents
register_test_agents() {
    log_info "Registering test agents..."
    
    # Build admin tool if needed
    local admin_binary="$PROJECT_ROOT/build/agentry-admin"
    if [[ ! -f "$admin_binary" ]]; then
        log_info "Building admin tool..."
        (cd "$PROJECT_ROOT" && make build-admin)
    fi
    
    for domain in "${!DOMAINS[@]}"; do
        port="${DOMAINS[$domain]}"
        
        log_info "Registering test agents for $domain"
        
        # Set admin endpoint
        export AMTP_ADMIN_ENDPOINT="http://localhost:$port"
        
        # Register test agents
        "$admin_binary" agent register sales --mode pull || log_warning "Failed to register sales agent for $domain"
        "$admin_binary" agent register support --mode pull || log_warning "Failed to register support agent for $domain"
        "$admin_binary" agent register api --mode pull || log_warning "Failed to register api agent for $domain"
        
        log_success "Test agents registered for $domain"
    done
}

# Main function
main() {
    case "${1:-}" in
        "start")
            check_prerequisites
            start_all
            ;;
        "stop")
            stop_all
            ;;
        "restart")
            stop_all
            sleep 2
            check_prerequisites
            start_all
            ;;
        "status")
            show_status
            ;;
        "test")
            test_gateways
            ;;
        "logs")
            if [[ -n "${2:-}" ]]; then
                show_logs "$2"
            else
                log_error "Please specify a domain for logs"
                log_info "Available domains: ${!DOMAINS[*]}"
                exit 1
            fi
            ;;
        "register-agents")
            register_test_agents
            ;;
        *)
            echo "Usage: $0 {start|stop|restart|status|test|logs <domain>|register-agents}"
            echo
            echo "Commands:"
            echo "  start           - Start all gateways"
            echo "  stop            - Stop all gateways"
            echo "  restart         - Restart all gateways"
            echo "  status          - Show status of all gateways"
            echo "  test            - Test all gateways"
            echo "  logs <domain>   - Show logs for a specific domain"
            echo "  register-agents - Register test agents for all domains"
            echo
            echo "Available domains: ${!DOMAINS[*]}"
            exit 1
            ;;
    esac
}

# Run main function with all arguments
main "$@"
