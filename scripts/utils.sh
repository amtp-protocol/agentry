#!/bin/bash

# AMTP Gateway Common Utilities
# This file contains shared functions for test scripts

set -euo pipefail

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

# Function to build Docker image
build_image() {
    local compose_file="$1"
    log_info "Building AMTP Gateway Docker image..."
    docker-compose -f "$compose_file" build
    log_success "Image built successfully"
}

# Function to start services with health checks
start_services() {
    local compose_file="$1"
    local expected_healthy_count="${2:-1}"

    log_step "Starting AMTP Gateway services..."

    log_info "Building and starting services..."
    docker-compose -f "$compose_file" up -d --build

    log_info "Waiting for services to be healthy..."
    local max_attempts=30
    local attempt=1

    while [ $attempt -le $max_attempts ]; do
        if docker-compose -f "$compose_file" ps | grep -q "healthy"; then
            local healthy_count=$(docker-compose -f "$compose_file" ps | grep -c "healthy" || echo "0")
            if [ "$healthy_count" -ge "$expected_healthy_count" ]; then
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
        docker-compose -f "$compose_file" ps
        return 1
    fi
}

# Function to test basic connectivity
test_connectivity() {
    local compose_file="$1"
    local domains=("${@:2}") # Get all arguments after the first

    log_step "Testing basic connectivity..."

    for domain in "${domains[@]}"; do
        log_info "Testing $domain health endpoint..."
        if docker-compose -f "$compose_file" exec -T test-client curl -f -s "http://$domain/health" > /dev/null; then
            log_success "✓ $domain is responding"
        else
            log_error "✗ $domain is not responding"
            return 1
        fi
    done
}

# Function to show service logs
show_logs() {
    local compose_file="$1"
    local services=("${@:2}")

    log_step "Showing recent service logs..."

    if [ ${#services[@]} -eq 0 ]; then
        log_info "All service logs:"
        docker-compose -f "$compose_file" logs --tail=20
    else
        for service in "${services[@]}"; do
            log_info "$service logs:"
            docker-compose -f "$compose_file" logs --tail=20 "$service"
            echo ""
        done
    fi
}

# Function to stop services
stop_services() {
    local compose_file="$1"
    log_step "Stopping services..."
    docker-compose -f "$compose_file" down -v
    log_success "Services stopped and cleaned up"
}

# Function to show service status
show_status() {
    local compose_file="$1"
    log_step "Service Status:"
    docker-compose -f "$compose_file" ps
    echo ""
}

# Function to get script and project directories
setup_paths() {
    local script_path="$1"
    local script_dir="$(cd "$(dirname "$script_path")" && pwd)"
    local project_root="$(dirname "$script_dir")"

    echo "$script_dir:$project_root"
}