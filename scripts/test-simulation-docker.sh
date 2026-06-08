#!/bin/bash

# AMTP Gateway Comprehensive Simulation Test Script (Docker Version)
# This script demonstrates both domain simulation and agent-centric schema functionality

set -euo pipefail

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
NETWORK_NAME="amtp-simulation-network"
IMAGE_NAME="amtp-gateway-simulation"

# Source common utilities
source "$SCRIPT_DIR/utils.sh"

# Function to test DNS discovery
test_dns_discovery() {
    log_step "Testing DNS TXT record discovery..."
    
    log_info "Testing DNS TXT record queries (schemas should NOT be in DNS)..."
    local domains=("company-a.local" "company-b.local" "partner.local")
    
    for domain in "${domains[@]}"; do
        log_info "Querying DNS TXT record for _amtp.$domain..."
        local txt_record=$(docker-compose -f "$PROJECT_ROOT/docker/docker-compose.domain-simulation.yml" exec -T test-client dig +short @172.20.0.10 "_amtp.$domain" TXT || echo "")
        if [ -n "$txt_record" ]; then
            log_success "DNS TXT record found for $domain: $txt_record"
            # Verify no schemas in DNS record
            if echo "$txt_record" | grep -q "schemas="; then
                log_error "❌ Found schemas in DNS TXT record (should be removed!)"
            else
                log_success "✓ No schemas found in DNS TXT record (correct!)"
            fi
        else
            log_error "DNS TXT record not found for $domain"
        fi
        echo ""
    done
}

# Function to create temporary schema directory and files
create_temp_schemas() {
    log_info "Creating temporary schema directory for file-based schema management..."
    
    # Create temporary directory
    mkdir -p /tmp/amtp-schemas
    mkdir -p /tmp/amtp-keys
    
    # Create sample schema files for file-based registry (optional)
    cat > /tmp/amtp-schemas/commerce.order.v1.json << 'EOF'
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "title": "Commerce Order",
  "type": "object",
  "properties": {
    "order_id": {"type": "string"},
    "customer_id": {"type": "string"},
    "total_amount": {"type": "number", "minimum": 0},
    "currency": {"type": "string", "enum": ["USD", "EUR", "GBP"]}
  },
  "required": ["order_id", "customer_id", "total_amount", "currency"]
}
EOF

    cat > /tmp/amtp-schemas/finance.payment.v1.json << 'EOF'
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "title": "Finance Payment",
  "type": "object",
  "properties": {
    "payment_id": {"type": "string"},
    "amount": {"type": "number", "minimum": 0},
    "currency": {"type": "string", "enum": ["USD", "EUR", "GBP"]},
    "status": {"type": "string", "enum": ["pending", "completed", "failed"]}
  },
  "required": ["payment_id", "amount", "currency", "status"]
}
EOF

    log_success "Temporary schema files created in /tmp/amtp-schemas/"
}

# Function to register schemas on all gateways
register_schemas() {
    log_step "Registering test schemas on all gateways..."
    
    local domains=("company-a.local:8080" "company-b.local:8080" "partner.local:8080")
    
    for domain in "${domains[@]}"; do
        log_info "Registering schemas on $domain..."
        
        # Register commerce.order.v1 schema
        log_info "  - Registering agntcy:commerce.order.v1..."
        docker-compose -f "$PROJECT_ROOT/docker/docker-compose.domain-simulation.yml" exec -T test-client curl -X POST "http://$domain/v1/admin/schemas" \
            -H "Content-Type: application/json" \
            -d '{
                "id": "agntcy:commerce.order.v1",
                "definition": {
                    "$schema": "http://json-schema.org/draft-07/schema#",
                    "title": "Commerce Order",
                    "type": "object",
                    "properties": {
                        "order_id": {"type": "string"},
                        "customer_id": {"type": "string"},
                        "items": {
                            "type": "array",
                            "items": {
                                "type": "object",
                                "properties": {
                                    "product_id": {"type": "string"},
                                    "quantity": {"type": "integer", "minimum": 1},
                                    "price": {"type": "number", "minimum": 0}
                                },
                                "required": ["product_id", "quantity", "price"]
                            }
                        },
                        "total_amount": {"type": "number", "minimum": 0},
                        "currency": {"type": "string", "enum": ["USD", "EUR", "GBP"]}
                    },
                    "required": ["order_id", "customer_id", "items", "total_amount", "currency"]
                }
            }' | format_json
        echo ""
        
        # Register finance.payment.v1 schema
        log_info "  - Registering agntcy:finance.payment.v1..."
        docker-compose -f "$PROJECT_ROOT/docker/docker-compose.domain-simulation.yml" exec -T test-client curl -X POST "http://$domain/v1/admin/schemas" \
            -H "Content-Type: application/json" \
            -d '{
                "id": "agntcy:finance.payment.v1",
                "definition": {
                    "$schema": "http://json-schema.org/draft-07/schema#",
                    "title": "Finance Payment",
                    "type": "object",
                    "properties": {
                        "payment_id": {"type": "string"},
                        "order_id": {"type": "string"},
                        "amount": {"type": "number", "minimum": 0},
                        "currency": {"type": "string", "enum": ["USD", "EUR", "GBP"]},
                        "payment_method": {"type": "string", "enum": ["credit_card", "bank_transfer", "paypal", "crypto"]},
                        "status": {"type": "string", "enum": ["pending", "completed", "failed", "refunded"]},
                        "timestamp": {"type": "string", "format": "date-time"}
                    },
                    "required": ["payment_id", "order_id", "amount", "currency", "payment_method", "status", "timestamp"]
                }
            }' | format_json
        echo ""
        
        # Register logistics.shipment.v1 schema
        log_info "  - Registering agntcy:logistics.shipment.v1..."
        docker-compose -f "$PROJECT_ROOT/docker/docker-compose.domain-simulation.yml" exec -T test-client curl -X POST "http://$domain/v1/admin/schemas" \
            -H "Content-Type: application/json" \
            -d '{
                "id": "agntcy:logistics.shipment.v1",
                "definition": {
                    "$schema": "http://json-schema.org/draft-07/schema#",
                    "title": "Logistics Shipment",
                    "type": "object",
                    "properties": {
                        "shipment_id": {"type": "string"},
                        "order_id": {"type": "string"},
                        "tracking_number": {"type": "string"},
                        "carrier": {"type": "string", "enum": ["fedex", "ups", "dhl", "usps"]},
                        "origin": {
                            "type": "object",
                            "properties": {
                                "address": {"type": "string"},
                                "city": {"type": "string"},
                                "country": {"type": "string"}
                            },
                            "required": ["address", "city", "country"]
                        },
                        "destination": {
                            "type": "object",
                            "properties": {
                                "address": {"type": "string"},
                                "city": {"type": "string"},
                                "country": {"type": "string"}
                            },
                            "required": ["address", "city", "country"]
                        },
                        "status": {"type": "string", "enum": ["pending", "in_transit", "delivered", "returned"]}
                    },
                    "required": ["shipment_id", "order_id", "tracking_number", "carrier", "origin", "destination", "status"]
                }
            }' | format_json
        echo ""
    done
}

# Function to register agents with schema support
register_agents_with_schemas() {
    log_step "Registering agents with schema support (demonstrating agent-centric schema model)..."
    
    # Company A agents (E-commerce focused)
    log_info "Registering agents on company-a.local (E-commerce focused)..."
    
    # Sales agent - supports commerce and finance (wildcards)
    log_info "  - Registering sales agent (supports commerce.* and finance.payment.*)..."
    local sales_response=$(docker-compose -f "$PROJECT_ROOT/docker/docker-compose.domain-simulation.yml" exec -T test-client curl -X POST "http://company-a.local:8080/v1/admin/agents" \
        -H "Content-Type: application/json" \
        -d '{
            "address": "sales",
            "delivery_mode": "pull",
            "supported_schemas": ["agntcy:commerce.*", "agntcy:finance.payment.*"]
        }')
    echo "$sales_response" | format_json
    echo "$sales_response" | jq -r '.agent.api_key' > /tmp/amtp-keys/sales.key
    echo ""
    
    # Order processing agent - only commerce orders (using pull mode for testing)
    log_info "  - Registering order-processor agent (supports only commerce.order.v1)..."
    local order_response=$(docker-compose -f "$PROJECT_ROOT/docker/docker-compose.domain-simulation.yml" exec -T test-client curl -X POST "http://company-a.local:8080/v1/admin/agents" \
        -H "Content-Type: application/json" \
        -d '{
            "address": "order-processor",
            "delivery_mode": "pull",
            "supported_schemas": ["agntcy:commerce.order.v1"]
        }')
    echo "$order_response" | format_json
    echo "$order_response" | jq -r '.agent.api_key' > /tmp/amtp-keys/order-processor.key
    echo ""
    
    # Company B agents (Finance focused)
    log_info "Registering agents on company-b.local (Finance focused)..."
    
    # Payment processor - finance payments (wildcard)
    log_info "  - Registering payment-processor agent (supports finance.*)..."
    local payment_response=$(docker-compose -f "$PROJECT_ROOT/docker/docker-compose.domain-simulation.yml" exec -T test-client curl -X POST "http://company-b.local:8080/v1/admin/agents" \
        -H "Content-Type: application/json" \
        -d '{
            "address": "payment-processor",
            "delivery_mode": "pull",
            "supported_schemas": ["agntcy:finance.*"]
        }')
    echo "$payment_response" | format_json
    echo "$payment_response" | jq -r '.agent.api_key' > /tmp/amtp-keys/payment-processor.key
    echo ""
    
    # Accounting agent - specific finance payment schema (using pull mode for testing)
    log_info "  - Registering accounting agent (supports finance.payment.v1 only)..."
    local accounting_response=$(docker-compose -f "$PROJECT_ROOT/docker/docker-compose.domain-simulation.yml" exec -T test-client curl -X POST "http://company-b.local:8080/v1/admin/agents" \
        -H "Content-Type: application/json" \
        -d '{
            "address": "accounting",
            "delivery_mode": "pull",
            "supported_schemas": ["agntcy:finance.payment.v1"]
        }')
    echo "$accounting_response" | format_json
    echo "$accounting_response" | jq -r '.agent.api_key' > /tmp/amtp-keys/accounting.key
    echo ""
    
    # Partner agents (Logistics focused)
    log_info "Registering agents on partner.local (Logistics focused)..."
    
    # Shipping agent - logistics only
    log_info "  - Registering shipping agent (supports logistics.*)..."
    local shipping_response=$(docker-compose -f "$PROJECT_ROOT/docker/docker-compose.domain-simulation.yml" exec -T test-client curl -X POST "http://partner.local:8080/v1/admin/agents" \
        -H "Content-Type: application/json" \
        -d '{
            "address": "shipping",
            "delivery_mode": "pull",
            "supported_schemas": ["agntcy:logistics.*"]
        }')
    echo "$shipping_response" | format_json
    echo "$shipping_response" | jq -r '.agent.api_key' > /tmp/amtp-keys/shipping.key
    echo ""
    
    # Integration agent - supports all schemas (empty array means all, using pull mode for testing)
    log_info "  - Registering integration agent (supports all schemas)..."
    local integration_response=$(docker-compose -f "$PROJECT_ROOT/docker/docker-compose.domain-simulation.yml" exec -T test-client curl -X POST "http://partner.local:8080/v1/admin/agents" \
        -H "Content-Type: application/json" \
        -d '{
            "address": "integration",
            "delivery_mode": "pull",
            "supported_schemas": []
        }')
    echo "$integration_response" | format_json
    echo "$integration_response" | jq -r '.agent.api_key' > /tmp/amtp-keys/integration.key
    echo ""
}

# Function to test gateway capabilities (should show agent schemas, not DNS schemas)
test_gateway_capabilities() {
    log_step "Testing gateway capabilities (should show schemas from agents, not DNS)..."
    
    local domains=("company-a.local:8080" "company-b.local:8080" "partner.local:8080")
    
    for domain in "${domains[@]}"; do
        log_info "Testing capabilities for $domain:"
        docker-compose -f "$PROJECT_ROOT/docker/docker-compose.domain-simulation.yml" exec -T test-client curl -s "http://$domain/v1/capabilities/$domain" | format_json
        echo ""
    done
}

# Function to test agent discovery with schemas
test_agent_discovery() {
    log_step "Testing agent discovery with schema information..."
    
    local domains=("company-a.local:8080" "company-b.local:8080" "partner.local:8080")
    
    for domain in "${domains[@]}"; do
        log_info "Testing agent discovery for $domain (should show supported schemas):"
        docker-compose -f "$PROJECT_ROOT/docker/docker-compose.domain-simulation.yml" exec -T test-client curl -s "http://$domain/v1/discovery/agents" | format_json
        echo ""
    done
}

# Function to test inter-gateway communication with schemas
test_inter_gateway_communication() {
    log_step "Testing inter-gateway communication with schema validation..."
    
    log_info "Company A discovering Company B capabilities:"
    docker-compose -f "$PROJECT_ROOT/docker/docker-compose.domain-simulation.yml" exec -T test-client curl -s "http://company-a.local:8080/v1/capabilities/company-b.local" | format_json
    echo ""
    
    log_info "Company B discovering Partner capabilities:"
    docker-compose -f "$PROJECT_ROOT/docker/docker-compose.domain-simulation.yml" exec -T test-client curl -s "http://company-b.local:8080/v1/capabilities/partner.local" | format_json
    echo ""
    
    log_info "Partner discovering Company A capabilities:"
    docker-compose -f "$PROJECT_ROOT/docker/docker-compose.domain-simulation.yml" exec -T test-client curl -s "http://partner.local:8080/v1/capabilities/company-a.local" | format_json
    echo ""
}

# Function to test schema validation scenarios
test_schema_validation() {
    log_step "Testing schema validation with agent-specific support..."
    
    # Test 1: Valid message with supported schema (wildcard match)
    log_info "Test 1: Sending commerce order to sales agent (should succeed - wildcard match)..."
    docker-compose -f "$PROJECT_ROOT/docker/docker-compose.domain-simulation.yml" exec -T test-client curl -X POST "http://company-a.local:8080/v1/messages" \
        -H "Content-Type: application/json" \
        -d '{
            "sender": "system@company-a.local",
            "recipients": ["sales@company-a.local"],
            "subject": "New Order",
            "schema": "agntcy:commerce.order.v1",
            "payload": {
                "order_id": "ORD-12345",
                "customer_id": "CUST-67890",
                "items": [
                    {
                        "product_id": "PROD-001",
                        "quantity": 2,
                        "price": 29.99
                    }
                ],
                "total_amount": 59.98,
                "currency": "USD"
            }
        }' | format_json
    echo ""
    
    # Test 2: Valid message with exact schema match
    log_info "Test 2: Sending commerce order to order-processor (should succeed - exact match)..."
    docker-compose -f "$PROJECT_ROOT/docker/docker-compose.domain-simulation.yml" exec -T test-client curl -X POST "http://company-a.local:8080/v1/messages" \
        -H "Content-Type: application/json" \
        -d '{
            "sender": "system@company-a.local",
            "recipients": ["order-processor@company-a.local"],
            "subject": "Process Order",
            "schema": "agntcy:commerce.order.v1",
            "payload": {
                "order_id": "ORD-67890",
                "customer_id": "CUST-12345",
                "items": [
                    {
                        "product_id": "PROD-002",
                        "quantity": 1,
                        "price": 149.99
                    }
                ],
                "total_amount": 149.99,
                "currency": "EUR"
            }
        }' | format_json
    echo ""
    
    # Test 3: Invalid message - agent doesn'\''t support schema
    log_info "Test 3: Sending finance payment to order-processor (should fail - schema not supported)..."
    docker-compose -f "$PROJECT_ROOT/docker/docker-compose.domain-simulation.yml" exec -T test-client curl -X POST "http://company-a.local:8080/v1/messages" \
        -H "Content-Type: application/json" \
        -d '{
            "sender": "system@company-a.local",
            "recipients": ["order-processor@company-a.local"],
            "subject": "Payment Notification",
            "schema": "agntcy:finance.payment.v1",
            "payload": {
                "payment_id": "PAY-12345",
                "order_id": "ORD-12345",
                "amount": 59.98,
                "currency": "USD",
                "payment_method": "credit_card",
                "status": "completed",
                "timestamp": "'$(date -Iseconds)'"
            }
        }' | format_json
    echo ""
    
    # Test 4: Cross-domain message with schema validation
    log_info "Test 4: Cross-domain message (Company A to Company B payment processor)..."
    docker-compose -f "$PROJECT_ROOT/docker/docker-compose.domain-simulation.yml" exec -T test-client curl -X POST "http://company-a.local:8080/v1/messages" \
        -H "Content-Type: application/json" \
        -d '{
            "sender": "sales@company-a.local",
            "recipients": ["payment-processor@company-b.local"],
            "subject": "Payment Request",
            "schema": "agntcy:finance.payment.v1",
            "payload": {
                "payment_id": "PAY-CROSS-001",
                "order_id": "ORD-CROSS-001",
                "amount": 199.99,
                "currency": "USD",
                "payment_method": "bank_transfer",
                "status": "pending",
                "timestamp": "'$(date -Iseconds)'"
            }
        }' | format_json
    echo ""
    
    # Test 5: Message to agent with no schema restrictions (supports all)
    log_info "Test 5: Sending logistics message to integration agent (should succeed - supports all)..."
    docker-compose -f "$PROJECT_ROOT/docker/docker-compose.domain-simulation.yml" exec -T test-client curl -X POST "http://partner.local:8080/v1/messages" \
        -H "Content-Type: application/json" \
        -d '{
            "sender": "system@partner.local",
            "recipients": ["integration@partner.local"],
            "subject": "Shipment Update",
            "schema": "agntcy:logistics.shipment.v1",
            "payload": {
                "shipment_id": "SHIP-67890",
                "order_id": "ORD-67890",
                "tracking_number": "1Z999BB1234567890",
                "carrier": "fedex",
                "origin": {
                    "address": "123 Warehouse St",
                    "city": "New York",
                    "country": "USA"
                },
                "destination": {
                    "address": "456 Customer Ave",
                    "city": "Los Angeles",
                    "country": "USA"
                },
                "status": "delivered"
            }
        }' | format_json
    echo ""
}

# Function to test inbox message retrieval for pull-mode agents
test_inbox_retrieval() {
    log_step "Testing inbox message retrieval for pull-mode agents..."
    
    # Wait a moment for message processing to complete
    sleep 2
    
    # Test 1: Check sales agent inbox (should have commerce order message)
    log_info "Test 1: Checking sales agent inbox (should have commerce order)..."
    local sales_response=$(cat /tmp/amtp-keys/sales.key)
    if [ -n "$sales_response" ] && [ "$sales_response" != "null" ]; then
        docker-compose -f "$PROJECT_ROOT/docker/docker-compose.domain-simulation.yml" exec -T test-client curl -s "http://company-a.local:8080/v1/inbox/sales@company-a.local" \
            -H "Authorization: Bearer $sales_response" | format_json
    else
        log_warning "Could not retrieve sales agent API key for inbox access"
    fi
    echo ""
    
    # Test 2: Check order-processor agent inbox (should have commerce order message)
    log_info "Test 2: Checking order-processor agent inbox (should have commerce order)..."
    local processor_response=$(cat /tmp/amtp-keys/order-processor.key)
    if [ -n "$processor_response" ] && [ "$processor_response" != "null" ]; then
        docker-compose -f "$PROJECT_ROOT/docker/docker-compose.domain-simulation.yml" exec -T test-client curl -s "http://company-a.local:8080/v1/inbox/order-processor@company-a.local" \
            -H "Authorization: Bearer $processor_response" | format_json
    else
        log_warning "Could not retrieve order-processor agent API key for inbox access"
    fi
    echo ""
    
    # Test 3: Check payment-processor agent inbox (should have finance payment message)
    log_info "Test 3: Checking payment-processor agent inbox (should have finance payment)..."
    local payment_response=$(cat /tmp/amtp-keys/payment-processor.key)
    if [ -n "$payment_response" ] && [ "$payment_response" != "null" ]; then
        docker-compose -f "$PROJECT_ROOT/docker/docker-compose.domain-simulation.yml" exec -T test-client curl -s "http://company-b.local:8080/v1/inbox/payment-processor@company-b.local" \
            -H "Authorization: Bearer $payment_response" | format_json
    else
        log_warning "Could not retrieve payment-processor agent API key for inbox access"
    fi
    echo ""
    
    # Test 4: Check accounting agent inbox (should be empty - finance payment was rejected)
    log_info "Test 4: Checking accounting agent inbox (should be empty - schema mismatch)..."
    local accounting_response=$(cat /tmp/amtp-keys/accounting.key)
    if [ -n "$accounting_response" ] && [ "$accounting_response" != "null" ]; then
        docker-compose -f "$PROJECT_ROOT/docker/docker-compose.domain-simulation.yml" exec -T test-client curl -s "http://company-b.local:8080/v1/inbox/accounting@company-b.local" \
            -H "Authorization: Bearer $accounting_response" | format_json
    else
        log_warning "Could not retrieve accounting agent API key for inbox access"
    fi
    echo ""
    
    # Test 5: Check integration agent inbox (should have logistics message)
    log_info "Test 5: Checking integration agent inbox (should have logistics message)..."
    local integration_response=$(cat /tmp/amtp-keys/integration.key)
    if [ -n "$integration_response" ] && [ "$integration_response" != "null" ]; then
        docker-compose -f "$PROJECT_ROOT/docker/docker-compose.domain-simulation.yml" exec -T test-client curl -s "http://partner.local:8080/v1/inbox/integration@partner.local" \
            -H "Authorization: Bearer $integration_response" | format_json
    else
        log_warning "Could not retrieve integration agent API key for inbox access"
    fi
    echo ""
}

# Function to test message acknowledgment
test_message_acknowledgment() {
    log_step "Testing message acknowledgment for pull-mode agents..."
    
    # Test acknowledging a message from the integration agent inbox
    log_info "Test: Acknowledging first message from integration agent inbox..."
    local integration_response=$(cat /tmp/amtp-keys/integration.key)
    if [ -n "$integration_response" ] && [ "$integration_response" != "null" ]; then
        # Get the first message ID from inbox
        local message_id=$(docker-compose -f "$PROJECT_ROOT/docker/docker-compose.domain-simulation.yml" exec -T test-client curl -s "http://partner.local:8080/v1/inbox/integration@partner.local" \
            -H "Authorization: Bearer $integration_response" | jq -r 'if .messages and (.messages | length > 0) then .messages[0].message_id else empty end')
        
        if [ -n "$message_id" ] && [ "$message_id" != "null" ]; then
            log_info "  - Acknowledging message ID: $message_id"
            docker-compose -f "$PROJECT_ROOT/docker/docker-compose.domain-simulation.yml" exec -T test-client curl -X DELETE "http://partner.local:8080/v1/inbox/integration@partner.local/$message_id" \
                -H "Authorization: Bearer $integration_response" | format_json
            echo ""
            
            # Verify message was removed
            log_info "  - Verifying message was removed from inbox..."
            docker-compose -f "$PROJECT_ROOT/docker/docker-compose.domain-simulation.yml" exec -T test-client curl -s "http://partner.local:8080/v1/inbox/integration@partner.local" \
                -H "Authorization: Bearer $integration_response" | format_json
        else
            log_warning "No messages found in integration agent inbox to acknowledge"
        fi
    else
        log_warning "Could not retrieve integration agent API key for acknowledgment test"
    fi
    echo ""
}

# Function to test invalid scenarios
test_invalid_scenarios() {
    log_step "Testing invalid schema scenarios..."
    
    # Test 1: Try to register agent with non-existent schema
    log_info "Test 1: Registering agent with non-existent schema (should fail)..."
    docker-compose -f "$PROJECT_ROOT/docker/docker-compose.domain-simulation.yml" exec -T test-client curl -X POST "http://company-a.local:8080/v1/admin/agents" \
        -H "Content-Type: application/json" \
        -d '{
            "address": "invalid-agent",
            "delivery_mode": "pull",
            "supported_schemas": ["agntcy:nonexistent.schema.v1"]
        }' | format_json
    echo ""
    
    # Test 2: Try to register agent with malformed schema identifier
    log_info "Test 2: Registering agent with malformed schema identifier (should fail)..."
    docker-compose -f "$PROJECT_ROOT/docker/docker-compose.domain-simulation.yml" exec -T test-client curl -X POST "http://company-a.local:8080/v1/admin/agents" \
        -H "Content-Type: application/json" \
        -d '{
            "address": "malformed-agent",
            "delivery_mode": "pull",
            "supported_schemas": ["invalid-schema-format"]
        }' | format_json
    echo ""
    
    # Test 3: Send message with invalid schema
    log_info "Test 3: Sending message with non-existent schema (should fail)..."
    docker-compose -f "$PROJECT_ROOT/docker/docker-compose.domain-simulation.yml" exec -T test-client curl -X POST "http://company-a.local:8080/v1/messages" \
        -H "Content-Type: application/json" \
        -d '{
            "sender": "system@company-a.local",
            "recipients": ["sales@company-a.local"],
            "subject": "Invalid Schema Test",
            "schema": "agntcy:nonexistent.schema.v1",
            "payload": {
                "test": "data"
            }
        }' | format_json
    echo ""
}

# Function to test workflow coordination across multiple gateways
test_workflow_coordination() {
    log_step "Testing multi-gateway workflow coordination..."

    local sender="sales@company-a.local"
    local recip1="payment-processor@company-b.local"
    local recip2="integration@partner.local"
    
    local COMPOSE_FILE="$PROJECT_ROOT/docker/docker-compose.domain-simulation.yml"
    local TEST_CLIENT="test-client"

    log_info "Creating sequential workflow across domains from $sender to $recip1 and $recip2"
    
    # Send a sequential workflow message
    local response=$(docker-compose -f "$COMPOSE_FILE" exec -T $TEST_CLIENT curl -s -X POST "http://company-a.local:8080/v1/messages" \
        -H "Content-Type: application/json" \
        -d '{
            "sender": "'"$sender"'",
            "recipients": ["'"$recip1"'", "'"$recip2"'"],
            "subject": "Process Order and Ship",
            "schema": "agntcy:finance.payment.v1",
            "coordination": {
                "type": "sequential",
                "sequence": ["'"$recip1"'", "'"$recip2"'"],
                "stop_on_failure": true,
                "timeout": 60
            },
            "payload": {"order_id": "ORD-12345", "amount": 100.0, "currency": "USD", "destination": "Beijing"}
        }')
        
    local message_id=$(echo "$response" | jq -r '.message_id // empty')
    
    if [ -z "$message_id" ]; then
        log_error "Failed to create cross-domain sequential workflow. Response: $response"
        exit 1
    fi
    log_info "Sequential Workflow created. ID: $message_id"

    # Wait for processing queues to flush
    sleep 2
    
    local payment_key=$(cat /tmp/amtp-keys/payment-processor.key 2>/dev/null)
    local integration_key=$(cat /tmp/amtp-keys/integration.key 2>/dev/null)

    log_info "Verifying sequence logic before first agent responds..."
    local inbox1=$(docker-compose -f "$COMPOSE_FILE" exec -T $TEST_CLIENT curl -s "http://company-b.local:8080/v1/inbox/$recip1" -H "Authorization: Bearer $payment_key")
    echo "Inbox 1 contains:"; echo "$inbox1"; local has_msg1=$(echo "$inbox1" | jq "[.messages[]? | select(.message_id == \"$message_id\")] | length")
    if [ "$has_msg1" != "1" ]; then
        log_error "First agent $recip1 did not receive the sequential message."
        exit 1
    fi
    local inbox2=$(docker-compose -f "$COMPOSE_FILE" exec -T $TEST_CLIENT curl -s "http://partner.local:8080/v1/inbox/$recip2" -H "Authorization: Bearer $integration_key")
    local has_msg2=$(echo "$inbox2" | jq "[.messages[]? | select(.message_id == \"$message_id\")] | length")
    if [ "$has_msg2" != "0" ]; then
        log_error "Second agent $recip2 prematurely received the sequential message! (count: $has_msg2)"
        exit 1
    fi
    log_info "Verified: $recip1 received msg, $recip2 did not."

    # 1. First agent responses via its local domain
    log_info "payment-processor responding to workflow..."
    local reply1_response=$(docker-compose -f "$COMPOSE_FILE" exec -T $TEST_CLIENT curl -s -X POST "http://company-b.local:8080/v1/messages" \
        -H "Content-Type: application/json" \
        -d '{
            "sender": "'"$recip1"'",
            "recipients": ["'"$sender"'"],
            "subject": "Payment Processed",
            "schema": "agntcy:finance.payment.v1",
            "in_reply_to": "'"$message_id"'",
            "response_type": "workflow_response",
            "payload": {"payment": "PAY-123", "status": "completed"}
        }')
        
    local reply1_status=$(echo "$reply1_response" | jq -r '.status // empty')
    if [ "$reply1_status" != "queued" ] && [ "$reply1_status" != "delivered" ]; then
        log_error "First workflow response failed (expected queued). Response: $reply1_response"
        exit 1
    fi
    log_info "payment-processor response accepted."

    sleep 2 

    log_info "Verifying sequence logic after first agent responded..."
    local inbox2_after=$(docker-compose -f "$COMPOSE_FILE" exec -T $TEST_CLIENT curl -s "http://partner.local:8080/v1/inbox/$recip2" -H "Authorization: Bearer $integration_key")
    local has_msg2_after=$(echo "$inbox2_after" | jq "[.messages[]? | select(.message_id == \"$message_id\")] | length")
    if [ "$has_msg2_after" != "1" ]; then
        log_error "Second agent $recip2 did not receive the sequential message after first agent finished. (Response: $inbox2_after)"
        exit 1
    fi
    log_info "Verified: $recip2 received msg."

    # 2. Second agent responds via its own remote domain
    log_info "integration responding..."
    local reply2_response=$(docker-compose -f "$COMPOSE_FILE" exec -T $TEST_CLIENT curl -s -X POST "http://partner.local:8080/v1/messages" \
        -H "Content-Type: application/json" \
        -d '{
            "sender": "'"$recip2"'",
            "recipients": ["'"$sender"'"],
            "subject": "Order Shipped",
            "schema": "agntcy:commerce.order.v1",
            "in_reply_to": "'"$message_id"'",
            "response_type": "workflow_response",
            "payload": {"tracking_number": "TRK-987654", "status": "shipped"}
        }')
        
    local reply2_status=$(echo "$reply2_response" | jq -r '.status // empty')
    if [ "$reply2_status" != "queued" ] && [ "$reply2_status" != "delivered" ]; then
        log_error "Second workflow response failed (expected queued). Response: $reply2_response"
        exit 1
    fi
    log_info "integration response accepted."

    log_info "Creating conditional workflow: approve triggers $recip2, else triggers accounting@company-b.local"
    # Send a conditional workflow message
    local cond_response=$(docker-compose -f "$COMPOSE_FILE" exec -T $TEST_CLIENT curl -s -X POST "http://company-a.local:8080/v1/messages" \
        -H "Content-Type: application/json" \
        -d '{
            "sender": "'"$sender"'",
            "recipients": ["'"$recip1"'"],
            "subject": "Approve Contract",
            "schema": "agntcy:finance.payment.v1",
            "coordination": {
                "type": "conditional",
                "conditions": [
                    {
                        "if": "status == '\''approved'\''",
                        "then": ["'"$recip2"'"],
                        "else": ["accounting@company-b.local"]
                    }
                ],
                "stop_on_failure": true,
                "timeout": 60
            },
            "payload": {"payment": "PAY-COND-1", "amount": 5000.0, "currency": "USD"}
        }')
        
    local cond_message_id=$(echo "$cond_response" | jq -r '.message_id // empty')
    
    if [ -z "$cond_message_id" ]; then
        log_error "Failed to create conditional workflow. Response: $cond_response"
        exit 1
    fi
    log_info "Conditional Workflow created. ID: $cond_message_id"

    sleep 2

    # Agent 1 (payment-processor) responds with "approved"
    log_info "payment-processor responding with status=approved..."
    local cond_reply1_response=$(docker-compose -f "$COMPOSE_FILE" exec -T $TEST_CLIENT curl -s -X POST "http://company-b.local:8080/v1/messages" \
        -H "Content-Type: application/json" \
        -d '{
            "sender": "'"$recip1"'",
            "recipients": ["'"$sender"'"],
            "subject": "Contract Approved",
            "schema": "agntcy:finance.payment.v1",
            "in_reply_to": "'"$cond_message_id"'",
            "response_type": "workflow_response",
            "payload": {"payment": "PAY-COND-1", "status": "approved"}
        }')
    
    local cond_reply1_status=$(echo "$cond_reply1_response" | jq -r '.status // empty')
    if [ "$cond_reply1_status" != "queued" ] && [ "$cond_reply1_status" != "delivered" ]; then
        log_error "First conditional workflow response failed. Response: $cond_reply1_response"
        exit 1
    fi
    log_info "payment-processor conditionally approved."

    sleep 2

    # Check inbox to ensure 'integration' (recip2) got the message because it matched 'Then' branch
    log_info "Verifying conditional dispatch..."
    local cond_inbox_then=$(docker-compose -f "$COMPOSE_FILE" exec -T $TEST_CLIENT curl -s "http://partner.local:8080/v1/inbox/$recip2" -H "Authorization: Bearer $integration_key")
    local cond_has_msg_then=$(echo "$cond_inbox_then" | jq "[.messages[]? | select(.message_id == \"$cond_message_id\")] | length")
    
    if [ "$cond_has_msg_then" != "1" ]; then
        log_error "Conditional targets ('Then' branch - $recip2) did NOT receive the message! Inbox: $cond_inbox_then"
        exit 1
    fi

    # Check accounting inbox, should NOT have the message ('Else' branch)
    local accounting_key=$(cat /tmp/amtp-keys/accounting.key 2>/dev/null)
    local cond_inbox_else=$(docker-compose -f "$COMPOSE_FILE" exec -T $TEST_CLIENT curl -s "http://company-b.local:8080/v1/inbox/accounting@company-b.local" -H "Authorization: Bearer $accounting_key")
    local cond_has_msg_else=$(echo "$cond_inbox_else" | jq "[.messages[]? | select(.message_id == \"$cond_message_id\")] | length")
    
    if [ "$cond_has_msg_else" != "0" ]; then
        log_error "Conditional skipped targets ('Else' branch - accounting) prematurely received the message!"
        exit 1
    fi

    log_info "Verified conditional logic: 'Then' triggered, 'Else' skipped."

    log_info "Workflow Coordination tests completed."
}
# Main execution
main() {
    echo "🚀 AMTP Gateway Comprehensive Simulation Test"
    echo "============================================="
    echo "This script demonstrates:"
    echo "  • Multi-domain gateway simulation"
    echo "  • Agent-centric schema functionality"
    echo "  • Schema validation and routing"
    echo "  • Cross-domain communication"
    echo ""

    local compose_file="$PROJECT_ROOT/docker/docker-compose.domain-simulation.yml"
    local domains=("company-a.local:8080" "company-b.local:8080" "partner.local:8080")

    case "${1:-start}" in
        "start")
            create_temp_schemas
            start_services "$compose_file" 3
            test_connectivity "$compose_file" "${domains[@]}"
            test_dns_discovery
            register_schemas
            register_agents_with_schemas
            test_gateway_capabilities
            test_agent_discovery
            test_inter_gateway_communication
            test_schema_validation
            test_inbox_retrieval
            test_message_acknowledgment
            test_invalid_scenarios
            test_workflow_coordination
            log_success "🎉 Comprehensive simulation test completed!"
            log_info "💡 Services are still running. Use '$0 stop' to clean up."
            log_info "💡 Use '$0 logs' to view service logs."
            log_info "💡 Use '$0 status' to check service status."
            ;;
        "stop")
            stop_services "$compose_file"
            # Clean up temporary schema directory
            if [ -d "/tmp/amtp-schemas" ]; then
                log_info "Cleaning up temporary schema directory..."
                rm -rf /tmp/amtp-schemas
            fi
            ;;
        "logs")
            show_logs "$compose_file" "gateway-company-a" "gateway-company-b" "gateway-partner"
            ;;
        "status")
            show_status "$compose_file"
            ;;
        "test-schemas")
            test_schema_validation
            test_invalid_scenarios
            test_workflow_coordination
            ;;
        "test-discovery")
            test_gateway_capabilities
            test_agent_discovery
            test_inter_gateway_communication
            ;;
        "test-connectivity")
            test_connectivity "$compose_file" "${domains[@]}"
            test_dns_discovery
            ;;
        "test-inbox")
            test_inbox_retrieval
            test_message_acknowledgment
            ;;
        *)
            echo "Usage: $0 {start|stop|logs|status|test-schemas|test-discovery|test-connectivity|test-inbox}"
            echo ""
            echo "Commands:"
            echo "  start             - Start services and run full comprehensive test suite"
            echo "  stop              - Stop and clean up services"
            echo "  logs              - Show recent service logs"
            echo "  status            - Show service status"
            echo "  test-schemas      - Run schema validation tests only"
            echo "  test-discovery    - Run discovery tests only"
            echo "  test-connectivity - Run connectivity tests only"
            echo "  test-inbox        - Run inbox retrieval and acknowledgment tests only"
            exit 1
            ;;
    esac
}

# Run main function with all arguments
main "$@"
