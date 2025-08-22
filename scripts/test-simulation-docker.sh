#!/bin/bash

# AMTP Gateway Comprehensive Simulation Test Script (Docker Version)
# This script demonstrates both domain simulation and agent-centric schema functionality

set -euo pipefail

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
NETWORK_NAME="amtp-simulation-network"
IMAGE_NAME="amtp-gateway-simulation"

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
    docker-compose -f "$PROJECT_ROOT/docker/docker-compose.schema-test.yml" build
    log_success "Image built successfully"
}

# Function to create network and start services
start_services() {
    log_step "Starting AMTP Gateway comprehensive simulation..."
    
    log_info "Building and starting services with domain and schema support..."
    docker-compose -f "$PROJECT_ROOT/docker/docker-compose.schema-test.yml" up -d --build
    
    log_info "Waiting for services to be healthy..."
    local max_attempts=30
    local attempt=1
    
    while [ $attempt -le $max_attempts ]; do
        if docker-compose -f "$PROJECT_ROOT/docker/docker-compose.schema-test.yml" ps | grep -q "healthy"; then
            local healthy_count=$(docker-compose -f "$PROJECT_ROOT/docker/docker-compose.schema-test.yml" ps | grep -c "healthy" || echo "0")
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
        docker-compose -f "$PROJECT_ROOT/docker/docker-compose.schema-test.yml" ps
        return 1
    fi
}

# Function to test basic connectivity
test_connectivity() {
    log_step "Testing basic connectivity..."
    
    local domains=("company-a.local:8080" "company-b.local:8080" "partner.local:8080")
    
    for domain in "${domains[@]}"; do
        log_info "Testing $domain health endpoint..."
        if docker-compose -f "$PROJECT_ROOT/docker/docker-compose.schema-test.yml" exec -T test-client curl -f -s "http://$domain/health" > /dev/null; then
            log_success "✓ $domain is responding"
        else
            log_error "✗ $domain is not responding"
            return 1
        fi
    done
}

# Function to test DNS discovery
test_dns_discovery() {
    log_step "Testing DNS TXT record discovery..."
    
    log_info "Testing DNS TXT record queries (schemas should NOT be in DNS)..."
    local domains=("company-a.local" "company-b.local" "partner.local")
    
    for domain in "${domains[@]}"; do
        log_info "Querying DNS TXT record for _amtp.$domain..."
        local txt_record=$(docker-compose -f "$PROJECT_ROOT/docker/docker-compose.schema-test.yml" exec -T test-client dig +short @172.21.0.10 "_amtp.$domain" TXT || echo "")
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
        docker-compose -f "$PROJECT_ROOT/docker/docker-compose.schema-test.yml" exec -T test-client curl -X POST "http://$domain/v1/admin/schemas" \
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
        docker-compose -f "$PROJECT_ROOT/docker/docker-compose.schema-test.yml" exec -T test-client curl -X POST "http://$domain/v1/admin/schemas" \
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
        docker-compose -f "$PROJECT_ROOT/docker/docker-compose.schema-test.yml" exec -T test-client curl -X POST "http://$domain/v1/admin/schemas" \
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
    docker-compose -f "$PROJECT_ROOT/docker/docker-compose.schema-test.yml" exec -T test-client curl -X POST "http://company-a.local:8080/v1/admin/agents" \
        -H "Content-Type: application/json" \
        -d '{
            "address": "sales",
            "delivery_mode": "pull",
            "supported_schemas": ["agntcy:commerce.*", "agntcy:finance.payment.*"]
        }' | format_json
    echo ""
    
    # Order processing agent - only commerce orders
    log_info "  - Registering order-processor agent (supports only commerce.order.v1)..."
    docker-compose -f "$PROJECT_ROOT/docker/docker-compose.schema-test.yml" exec -T test-client curl -X POST "http://company-a.local:8080/v1/admin/agents" \
        -H "Content-Type: application/json" \
        -d '{
            "address": "order-processor",
            "delivery_mode": "push",
            "push_target": "https://company-a.local/webhook/orders",
            "supported_schemas": ["agntcy:commerce.order.v1"]
        }' | format_json
    echo ""
    
    # Company B agents (Finance focused)
    log_info "Registering agents on company-b.local (Finance focused)..."
    
    # Payment processor - finance payments (wildcard)
    log_info "  - Registering payment-processor agent (supports finance.*)..."
    docker-compose -f "$PROJECT_ROOT/docker/docker-compose.schema-test.yml" exec -T test-client curl -X POST "http://company-b.local:8080/v1/admin/agents" \
        -H "Content-Type: application/json" \
        -d '{
            "address": "payment-processor",
            "delivery_mode": "pull",
            "supported_schemas": ["agntcy:finance.*"]
        }' | format_json
    echo ""
    
    # Accounting agent - specific finance payment schema
    log_info "  - Registering accounting agent (supports finance.payment.v1 only)..."
    docker-compose -f "$PROJECT_ROOT/docker/docker-compose.schema-test.yml" exec -T test-client curl -X POST "http://company-b.local:8080/v1/admin/agents" \
        -H "Content-Type: application/json" \
        -d '{
            "address": "accounting",
            "delivery_mode": "push",
            "push_target": "https://company-b.local/webhook/accounting",
            "supported_schemas": ["agntcy:finance.payment.v1"]
        }' | format_json
    echo ""
    
    # Partner agents (Logistics focused)
    log_info "Registering agents on partner.local (Logistics focused)..."
    
    # Shipping agent - logistics only
    log_info "  - Registering shipping agent (supports logistics.*)..."
    docker-compose -f "$PROJECT_ROOT/docker/docker-compose.schema-test.yml" exec -T test-client curl -X POST "http://partner.local:8080/v1/admin/agents" \
        -H "Content-Type: application/json" \
        -d '{
            "address": "shipping",
            "delivery_mode": "pull",
            "supported_schemas": ["agntcy:logistics.*"]
        }' | format_json
    echo ""
    
    # Integration agent - supports all schemas (empty array means all)
    log_info "  - Registering integration agent (supports all schemas)..."
    docker-compose -f "$PROJECT_ROOT/docker/docker-compose.schema-test.yml" exec -T test-client curl -X POST "http://partner.local:8080/v1/admin/agents" \
        -H "Content-Type: application/json" \
        -d '{
            "address": "integration",
            "delivery_mode": "push",
            "push_target": "https://partner.local/webhook/integration",
            "supported_schemas": []
        }' | format_json
    echo ""
}

# Function to test gateway capabilities (should show agent schemas, not DNS schemas)
test_gateway_capabilities() {
    log_step "Testing gateway capabilities (should show schemas from agents, not DNS)..."
    
    local domains=("company-a.local:8080" "company-b.local:8080" "partner.local:8080")
    
    for domain in "${domains[@]}"; do
        log_info "Testing capabilities for $domain:"
        docker-compose -f "$PROJECT_ROOT/docker/docker-compose.schema-test.yml" exec -T test-client curl -s "http://$domain/v1/capabilities/$domain" | format_json
        echo ""
    done
}

# Function to test agent discovery with schemas
test_agent_discovery() {
    log_step "Testing agent discovery with schema information..."
    
    local domains=("company-a.local:8080" "company-b.local:8080" "partner.local:8080")
    
    for domain in "${domains[@]}"; do
        log_info "Testing agent discovery for $domain (should show supported schemas):"
        docker-compose -f "$PROJECT_ROOT/docker/docker-compose.schema-test.yml" exec -T test-client curl -s "http://$domain/v1/discovery/agents" | format_json
        echo ""
    done
}

# Function to test inter-gateway communication with schemas
test_inter_gateway_communication() {
    log_step "Testing inter-gateway communication with schema validation..."
    
    log_info "Company A discovering Company B capabilities:"
    docker-compose -f "$PROJECT_ROOT/docker/docker-compose.schema-test.yml" exec -T test-client curl -s "http://company-a.local:8080/v1/capabilities/company-b.local" | format_json
    echo ""
    
    log_info "Company B discovering Partner capabilities:"
    docker-compose -f "$PROJECT_ROOT/docker/docker-compose.schema-test.yml" exec -T test-client curl -s "http://company-b.local:8080/v1/capabilities/partner.local" | format_json
    echo ""
    
    log_info "Partner discovering Company A capabilities:"
    docker-compose -f "$PROJECT_ROOT/docker/docker-compose.schema-test.yml" exec -T test-client curl -s "http://partner.local:8080/v1/capabilities/company-a.local" | format_json
    echo ""
}

# Function to test schema validation scenarios
test_schema_validation() {
    log_step "Testing schema validation with agent-specific support..."
    
    # Test 1: Valid message with supported schema (wildcard match)
    log_info "Test 1: Sending commerce order to sales agent (should succeed - wildcard match)..."
    docker-compose -f "$PROJECT_ROOT/docker/docker-compose.schema-test.yml" exec -T test-client curl -X POST "http://company-a.local:8080/v1/messages" \
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
    docker-compose -f "$PROJECT_ROOT/docker/docker-compose.schema-test.yml" exec -T test-client curl -X POST "http://company-a.local:8080/v1/messages" \
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
    docker-compose -f "$PROJECT_ROOT/docker/docker-compose.schema-test.yml" exec -T test-client curl -X POST "http://company-a.local:8080/v1/messages" \
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
    docker-compose -f "$PROJECT_ROOT/docker/docker-compose.schema-test.yml" exec -T test-client curl -X POST "http://company-a.local:8080/v1/messages" \
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
    docker-compose -f "$PROJECT_ROOT/docker/docker-compose.schema-test.yml" exec -T test-client curl -X POST "http://partner.local:8080/v1/messages" \
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

# Function to test invalid scenarios
test_invalid_scenarios() {
    log_step "Testing invalid schema scenarios..."
    
    # Test 1: Try to register agent with non-existent schema
    log_info "Test 1: Registering agent with non-existent schema (should fail)..."
    docker-compose -f "$PROJECT_ROOT/docker/docker-compose.schema-test.yml" exec -T test-client curl -X POST "http://company-a.local:8080/v1/admin/agents" \
        -H "Content-Type: application/json" \
        -d '{
            "address": "invalid-agent",
            "delivery_mode": "pull",
            "supported_schemas": ["agntcy:nonexistent.schema.v1"]
        }' | format_json
    echo ""
    
    # Test 2: Try to register agent with malformed schema identifier
    log_info "Test 2: Registering agent with malformed schema identifier (should fail)..."
    docker-compose -f "$PROJECT_ROOT/docker/docker-compose.schema-test.yml" exec -T test-client curl -X POST "http://company-a.local:8080/v1/admin/agents" \
        -H "Content-Type: application/json" \
        -d '{
            "address": "malformed-agent",
            "delivery_mode": "pull",
            "supported_schemas": ["invalid-schema-format"]
        }' | format_json
    echo ""
    
    # Test 3: Send message with invalid schema
    log_info "Test 3: Sending message with non-existent schema (should fail)..."
    docker-compose -f "$PROJECT_ROOT/docker/docker-compose.schema-test.yml" exec -T test-client curl -X POST "http://company-a.local:8080/v1/messages" \
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

# Function to show service logs
show_logs() {
    log_step "Showing recent service logs..."
    
    log_info "Company A Gateway logs:"
    docker-compose -f "$PROJECT_ROOT/docker/docker-compose.schema-test.yml" logs --tail=20 gateway-company-a
    echo ""
    
    log_info "Company B Gateway logs:"
    docker-compose -f "$PROJECT_ROOT/docker/docker-compose.schema-test.yml" logs --tail=20 gateway-company-b
    echo ""
    
    log_info "Partner Gateway logs:"
    docker-compose -f "$PROJECT_ROOT/docker/docker-compose.schema-test.yml" logs --tail=20 gateway-partner
    echo ""
}

# Function to stop services
stop_services() {
    log_step "Stopping services..."
    docker-compose -f "$PROJECT_ROOT/docker/docker-compose.schema-test.yml" down -v
    
    # Clean up temporary schema directory
    if [ -d "/tmp/amtp-schemas" ]; then
        log_info "Cleaning up temporary schema directory..."
        rm -rf /tmp/amtp-schemas
    fi
    
    log_success "Services stopped and cleaned up"
}

# Function to show service status
show_status() {
    log_step "Service Status:"
    docker-compose -f "$PROJECT_ROOT/docker/docker-compose.schema-test.yml" ps
    echo ""
    
    log_info "Network information:"
    docker network ls | grep amtp-schema || echo "No AMTP simulation networks found"
    echo ""
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
    
    case "${1:-start}" in
        "start")
            create_temp_schemas
            start_services
            test_connectivity
            test_dns_discovery
            register_schemas
            register_agents_with_schemas
            test_gateway_capabilities
            test_agent_discovery
            test_inter_gateway_communication
            test_schema_validation
            test_invalid_scenarios
            log_success "🎉 Comprehensive simulation test completed!"
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
        "test-schemas")
            test_schema_validation
            test_invalid_scenarios
            ;;
        "test-discovery")
            test_gateway_capabilities
            test_agent_discovery
            test_inter_gateway_communication
            ;;
        "test-connectivity")
            test_connectivity
            test_dns_discovery
            ;;
        *)
            echo "Usage: $0 {start|stop|logs|status|test-schemas|test-discovery|test-connectivity}"
            echo ""
            echo "Commands:"
            echo "  start             - Start services and run full comprehensive test suite"
            echo "  stop              - Stop and clean up services"
            echo "  logs              - Show recent service logs"
            echo "  status            - Show service status"
            echo "  test-schemas      - Run schema validation tests only"
            echo "  test-discovery    - Run discovery tests only"
            echo "  test-connectivity - Run connectivity tests only"
            exit 1
            ;;
    esac
}

# Run main function with all arguments
main "$@"
