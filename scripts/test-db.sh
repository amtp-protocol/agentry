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

    # Cleanup: Try to delete the agent first in case it exists from a previous run
    # This ensures the test is idempotent and works even if the database persists
    log_info "Cleaning up any existing agent..."
    docker-compose -f "$PROJECT_ROOT/docker/docker-compose.db-test.yml" exec -T test-client curl -X DELETE -s "http://$domain/v1/admin/agents/user" > /dev/null 2>&1 || true

    # Generate a unique subject to trace this specific test run
    local unique_subject="Local Test Message $(date +%s)"

    # Register a agent to receive messages
    local register_response=$(docker-compose -f "$PROJECT_ROOT/docker/docker-compose.db-test.yml" exec -T test-client curl -X POST -s "http://$domain/v1/admin/agents" \
            -H "Content-Type: application/json" \
            -d '{
                "address": "user",
                "delivery_mode": "pull"
            }')
    
    local api_key=$(echo "$register_response" | jq -r '.agent.api_key // empty')
    if [ -z "$api_key" ]; then
        log_error "❌ Failed to register agent. Response: $register_response"
        return 1
    fi
    log_success "✓ Agent registered successfully"

    # Send a message to the agent
    local message_id=$(docker-compose -f "$PROJECT_ROOT/docker/docker-compose.db-test.yml" exec -T test-client curl -X POST "http://$domain/v1/messages" \
            -H "Content-Type: application/json" \
            -d '{
                    "sender": "test@localhost",
                    "recipients": ["user@localhost"],
                    "subject": "'"$unique_subject"'",
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

# Function to test schema lifecycle
test_schema_lifecycle() {
    log_step "Testing schema lifecycle with database storage enabled..."

    domain="agentry:8080"
    schema_id="agntcy:test-domain.user.v1"
    encoded_schema_id="agntcy:test-domain.user.v1"

    # Register a schema
    log_info "1. Registering schema..."
    local register_response=$(docker-compose -f "$PROJECT_ROOT/docker/docker-compose.db-test.yml" exec -T test-client curl -X POST -s "http://$domain/v1/admin/schemas" \
            -H "Content-Type: application/json" \
            -d '{
                "id": "'"$schema_id"'",
                "definition": {
                    "type": "record",
                    "name": "User",
                    "fields": [
                        {"name": "id", "type": "string"},
                        {"name": "name", "type": "string"}
                    ]
                }
            }')

    if echo "$register_response" | grep -q "error"; then
        log_error "❌ Failed to register schema: $register_response"
        return 1
    fi
    log_success "✓ Schema registered successfully"

    # Retrieve the schema
    log_info "2. Retrieving schema..."
    local get_response=$(docker-compose -f "$PROJECT_ROOT/docker/docker-compose.db-test.yml" exec -T test-client curl -s "http://$domain/v1/admin/schemas/$encoded_schema_id")

    if ! echo "$get_response" | grep -q "$schema_id"; then
        log_error "❌ Failed to retrieve schema or schema ID mismatch. Response: $get_response"
        return 1
    fi
    log_success "✓ Schema retrieved successfully"

    # List schemas
    log_info "3. Listing schemas..."
    local list_response=$(docker-compose -f "$PROJECT_ROOT/docker/docker-compose.db-test.yml" exec -T test-client curl -s "http://$domain/v1/admin/schemas?domain=test-domain")
    # Verify the response contains the expected domain and entity fields that make up the schema ID
    if ! echo "$list_response" | grep -q "\"domain\":\"test-domain\"" || ! echo "$list_response" | grep -q "\"entity\":\"user\""; then
        log_error "❌ Failed to list schemas or find registered schema. Response: $list_response"
        return 1
    fi
    log_success "✓ Schemas listed successfully"

    # Check schema stats via admin endpoint
    log_info "4. Checking schema stats..."
    local stats_response=$(docker-compose -f "$PROJECT_ROOT/docker/docker-compose.db-test.yml" exec -T test-client curl -s "http://$domain/v1/admin/schemas/stats")
    local total_schemas=$(echo "$stats_response" | jq -r '.stats.total_schemas // 0')
    if [ "$total_schemas" -lt 1 ]; then
        log_error "❌ Schema stats show zero schemas. Response: $stats_response"
        return 1
    fi
    log_success "✓ Schema stats reported: $total_schemas total_schemas"

    # Delete the schema
    log_info "5. Deleting schema..."
    local delete_response=$(docker-compose -f "$PROJECT_ROOT/docker/docker-compose.db-test.yml" exec -T test-client curl -X DELETE -s "http://$domain/v1/admin/schemas/$encoded_schema_id")

    if echo "$delete_response" | grep -q "error"; then
        log_error "❌ Failed to delete schema. Response: $delete_response"
        return 1
    fi
    log_success "✓ Schema deleted successfully"

    # Verify deletion
    log_info "6. Verifying deletion..."
    local verify_response=$(docker-compose -f "$PROJECT_ROOT/docker/docker-compose.db-test.yml" exec -T test-client curl -s -o /dev/null -w "%{http_code}" "http://$domain/v1/admin/schemas/$encoded_schema_id")

    if [ "$verify_response" != "404" ]; then
        log_error "❌ Schema still exists after deletion (HTTP $verify_response)"
        return 1
    fi
    log_success "✓ Deletion verified"
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
            start_services "$compose_file" 3
            test_connectivity "$compose_file" "agentry:8080"
            test_message_lifecycle
            test_schema_lifecycle
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
        "test-schema-lifecycle")
            test_schema_lifecycle
            ;;
        *)
            echo "Usage: $0 {start|stop|logs|status|test-connectivity|test-message-lifecycle|test-schema-lifecycle}"
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