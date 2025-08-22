#!/bin/bash

# Setup local hosts file for domain simulation
# This script helps configure /etc/hosts for local domain access

set -euo pipefail

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

# Domains to add
DOMAINS=(
    "company-a.local"
    "company-b.local"
    "partner.local"
)

# Function to add domains to hosts file
add_domains() {
    log_info "Adding domains to /etc/hosts..."
    
    # Backup hosts file
    sudo cp /etc/hosts /etc/hosts.backup.$(date +%Y%m%d_%H%M%S)
    log_info "Backed up /etc/hosts"
    
    # Add domains
    for domain in "${DOMAINS[@]}"; do
        if grep -q "$domain" /etc/hosts; then
            log_warning "$domain already exists in /etc/hosts"
        else
            echo "127.0.0.1 $domain" | sudo tee -a /etc/hosts > /dev/null
            log_success "Added $domain to /etc/hosts"
        fi
    done
    
    log_success "Domains added! You can now access:"
    echo "  - http://company-a.local:8080"
    echo "  - http://company-b.local:8081"
    echo "  - http://partner.local:8082"
}

# Function to remove domains from hosts file
remove_domains() {
    log_info "Removing domains from /etc/hosts..."
    
    # Backup hosts file
    sudo cp /etc/hosts /etc/hosts.backup.$(date +%Y%m%d_%H%M%S)
    log_info "Backed up /etc/hosts"
    
    # Remove domains
    for domain in "${DOMAINS[@]}"; do
        if grep -q "$domain" /etc/hosts; then
            sudo sed -i "/$domain/d" /etc/hosts
            log_success "Removed $domain from /etc/hosts"
        else
            log_warning "$domain not found in /etc/hosts"
        fi
    done
    
    log_success "Domains removed from /etc/hosts"
}

# Function to show current domains
show_domains() {
    log_info "Current AMTP domains in /etc/hosts:"
    for domain in "${DOMAINS[@]}"; do
        if grep -q "$domain" /etc/hosts; then
            grep "$domain" /etc/hosts
        else
            echo "$domain - NOT FOUND"
        fi
    done
}

# Main execution
case "${1:-help}" in
    "add")
        add_domains
        ;;
    "remove")
        remove_domains
        ;;
    "show")
        show_domains
        ;;
    *)
        echo "Usage: $0 {add|remove|show}"
        echo ""
        echo "Commands:"
        echo "  add    - Add AMTP domains to /etc/hosts"
        echo "  remove - Remove AMTP domains from /etc/hosts"
        echo "  show   - Show current AMTP domains in /etc/hosts"
        echo ""
        echo "Note: This script requires sudo privileges to modify /etc/hosts"
        exit 1
        ;;
esac
