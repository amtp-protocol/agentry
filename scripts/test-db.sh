#!/bin/bash

# AMTP Database Storage Test Script (Docker Version)
# This script demonstrates agentry functionality with database storage enabled

set -euo pipefail

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

# Source common utilities
source "$SCRIPT_DIR/utils.sh"

# Function to test message lifecycle
test_message_lifecycle() {
    log_step "Testing message lifecycle with database storage enabled..."

    domain="agentry:8080"

    # Register a agent to receive messages
    docker-compose -f "$PROJECT_ROOT/docker/docker-compose.db-test.yml" exec -T test-client curl -X POST -s -o /dev/null "http://$domain/v1/admin/agents" \
            -H "Content-Type: application/json" \
            -d '{
                "address": "user",
                "delivery_mode": "pull"
            }'
    local api_key=$(docker-compose -f "$PROJECT_ROOT/docker/docker-compose.db-test.yml" exec -T test-client curl -s "http://$domain/v1/admin/agents" | jq -r '.agents."user@localhost".api_key // empty')
    if [ -z "$api_key" ]; then
        log_error "❌ Failed to register agent"
        return 1
    fi
    log_success "✓ Agent registered successfully"

    # Send a message to the agent
    local message_id=$(docker-compose -f "$PROJECT_ROOT/docker/docker-compose.db-test.yml" exec -T test-client curl -X POST "http://$domain/v1/messages" \
            -H "Content-Type: application/json" \
            -d '{
                    "sender": "test@localhost",
                    "recipients": ["user@localhost"],
                    "subject": "Local Test Message",
                    "payload": {"message": "Hello localhost!"}
            }' | jq -r '.message_id // empty')
    if [ -z "$message_id" ]; then
        log_error "❌ Failed to send message"
        return 1
    fi
    log_success "✓ Test message sent successfully"

    # Get the message for the agent
    local message=$(docker-compose -f "$PROJECT_ROOT/docker/docker-compose.db-test.yml" exec -T test-client curl -X GET "http://$domain/v1/messages/$message_id" \
            -H "Content-Type: application/json" \
            -H "Authorization: Bearer $api_key")
    if [ -z "$message" ]; then
        log_error "❌ Failed to receive message"
        return 1
    fi
    log_success "✓ Test message received successfully"

    # Check inbox messages for the agent
    local inbox_messages=$(docker-compose -f "$PROJECT_ROOT/docker/docker-compose.db-test.yml" exec -T test-client curl -X GET "http://$domain/v1/inbox/user@localhost" \
            -H "Content-Type: application/json" \
            -H "Authorization: Bearer $api_key")
    if [ -z "$inbox_messages" ]; then
        log_error "❌ Failed to retrieve inbox messages"
        return 1
    fi
    if ! echo "$inbox_messages" | jq -e '.messages | any(.message_id == "'$message_id'")' > /dev/null; then
        log_error "❌ Test message not found in inbox"
        return 1
    fi
    log_success "✓ Inbox messages retrieved successfully"

    # Acknowledge the message
    docker-compose -f "$PROJECT_ROOT/docker/docker-compose.db-test.yml" exec -T test-client curl -X DELETE -s -o /dev/null "http://$domain/v1/inbox/user@localhost/$message_id" \
        -H "Content-Type: application/json" \
        -H "Authorization: Bearer $api_key"
    local acknowledged_status=$(docker-compose -f "$PROJECT_ROOT/docker/docker-compose.db-test.yml" exec -T test-client curl -X GET "http://$domain/v1/messages/$message_id/status" \
        -H "Content-Type: application/json" \
        -H "Authorization: Bearer $api_key" | jq -r '.recipients[0].acknowledged // empty')
    if [ "$acknowledged_status" != "true" ]; then
        log_error "❌ Failed to acknowledge message"
        return 1
    fi
    log_success "✓ Message acknowledged successfully"
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

    local compose_file="$PROJECT_ROOT/docker/docker-compose.db-test.yml"

    case "${1:-start}" in
        "start")
            start_services "$compose_file" 1
            test_connectivity "$compose_file" "agentry:8080"
            test_message_lifecycle
            log_success "🎉 Database storage test completed!"
            log_info "💡 Services are still running. Use '$0 stop' to clean up."
            log_info "💡 Use '$0 logs' to view service logs."
            log_info "💡 Use '$0 status' to check service status."
            ;;
        "stop")
            stop_services "$compose_file"
            ;;
        "logs")
            show_logs "$compose_file" "agentry"
            ;;
        "status")
            show_status "$compose_file"
            ;;
        "test-connectivity")
            test_connectivity "$compose_file" "agentry:8080"
            ;;
        "test-message-lifecycle")
            test_message_lifecycle
            ;;
        *)
            echo "Usage: $0 {start|stop|logs|status|test-connectivity|test-message-lifecycle}"
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