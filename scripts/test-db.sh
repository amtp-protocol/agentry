#!/bin/bash

# AMTP Database Storage Test Script (Docker Version)
# This script demonstrates agentry functionality with database storage enabled

set -euo pipefail

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
NETWORK_NAME="amtp-network"
IMAGE_NAME="amtp-gateway-db"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
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

log_step() {
    echo -e "${CYAN}[STEP]${NC} $1"
}

# Function to check if jq is available
check_jq() {
    if ! command -v jq &> /dev/null; then
        log_warning "jq not found. JSON output will not be formatted."
        return 1
    fi
    return 0
}

# Function to format JSON output
format_json() {
    if check_jq; then
        jq .
    else
        cat
    fi
}

# Function to build image
build_image() {
    log_info "Building AMTP Gateway Docker image..."
    docker-compose -f "$PROJECT_ROOT/docker/docker-compose.db-test.yml" build
    log_success "Image built successfully"
}

# Function to create network and start services
start_services() {
    log_step "Starting AMTP Gateway with database storage..."
    
    log_info "Building and starting services with database storage..."
    docker-compose -f "$PROJECT_ROOT/docker/docker-compose.db-test.yml" up -d --build
    
    log_info "Waiting for services to be healthy..."
    local max_attempts=30
    local attempt=1
    
    while [ $attempt -le $max_attempts ]; do
        if docker-compose -f "$PROJECT_ROOT/docker/docker-compose.db-test.yml" ps | grep -q "healthy"; then
            local healthy_count=$(docker-compose -f "$PROJECT_ROOT/docker/docker-compose.db-test.yml" ps | grep -c "healthy" || echo "0")
            if [ "$healthy_count" -ge 3 ]; then
                log_success "All services are healthy!"
                break
            fi
        fi
        
        log_info "Attempt $attempt/$max_attempts: Waiting for services to be ready..."
        sleep 3
        ((attempt++))
    done
    
    if [ $attempt -gt $max_attempts ]; then
        log_error "Services failed to become healthy within timeout"
        docker-compose -f "$PROJECT_ROOT/docker/docker-compose.db-test.yml" ps
        return 1
    fi
}

# Function to test basic connectivity
test_connectivity() {
    log_step "Testing basic connectivity..."
    
    local domain="localhost:8080"
    
    log_info "Testing $domain health endpoint..."
    if docker-compose -f "$PROJECT_ROOT/docker/docker-compose.db-test.yml" exec -T test-client curl -f -s "http://$domain/health" > /dev/null; then
        log_success "✓ $domain is responding"
    else
        log_error "✗ $domain is not responding"
        return 1
    fi
}

# Function to show service logs
show_logs() {
    log_step "Showing recent service logs..."
    
    log_info "Gateway logs:"
    docker-compose -f "$PROJECT_ROOT/docker/docker-compose.db-test.yml" logs --tail=20 agentry
    echo ""
}

# Function to stop services
stop_services() {
    log_step "Stopping services..."
    docker-compose -f "$PROJECT_ROOT/docker/docker-compose.db-test.yml" down -v
    
    log_success "Services stopped and cleaned up"
}

# Function to show service status
show_status() {
    log_step "Service Status:"
    docker-compose -f "$PROJECT_ROOT/docker/docker-compose.db-test.yml" ps
    echo ""
}

# Main execution
main() {
    echo "🚀 AMTP Gateway Database Storage Test"
    echo "============================================="
    echo "This script demonstrates:"
    echo "  • Database storage configuration"
    echo "  • Gateway connectivity with database storage enabled"
    echo "  • Message lifecycle management with database storage enabled"
    echo ""
    
    case "${1:-start}" in
        "start")
            start_services
            test_connectivity
            log_success "🎉 Database storage test completed!"
            log_info "💡 Services are still running. Use '$0 stop' to clean up."
            log_info "💡 Use '$0 logs' to view service logs."
            log_info "💡 Use '$0 status' to check service status."
            ;;
        "stop")
            stop_services
            ;;
        "logs")
            show_logs
            ;;
        "status")
            show_status
            ;;
        "test-connectivity")
            test_connectivity
            ;;
        *)
            echo "Usage: $0 {start|stop|logs|status|test-schemas|test-discovery|test-connectivity|test-inbox}"
            echo ""
            echo "Commands:"
            echo "  start             - Start services and run full comprehensive test suite"
            echo "  stop              - Stop and clean up services"
            echo "  logs              - Show recent service logs"
            echo "  status            - Show service status"
            echo "  test-connectivity - Run connectivity tests only"
            exit 1
            ;;
    esac
}

# Run main function with all arguments
main "$@"
