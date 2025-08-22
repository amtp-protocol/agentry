#!/bin/bash
# Local development configuration for AMTP Gateway
# This script sets up the gateway for local testing with localhost/test domains

export AMTP_TLS_ENABLED=false
export AMTP_SERVER_ADDRESS=":8080"
export AMTP_DOMAIN="localhost"
export AMTP_LOG_LEVEL="debug"
export AMTP_LOG_FORMAT="text"
export AMTP_AUTH_REQUIRED=false
export AMTP_MESSAGE_VALIDATION_ENABLED=true
export AMTP_DNS_MOCK_MODE=true
export AMTP_DNS_ALLOW_HTTP=true

echo "==============================================="
echo "🚀 Starting Agentry in development mode"
echo "==============================================="
echo ""
echo "📍 Server Configuration:"
echo "   • Address: http://localhost:8080"
echo "   • Domain: localhost"
echo "   • TLS: disabled"
echo "   • Auth: disabled"
echo "   • DNS: mock mode enabled"
echo "   • HTTP gateways: allowed for development"
echo ""
echo "🔗 Available Endpoints:"
echo "   • Health:     http://localhost:8080/health"
echo "   • Ready:      http://localhost:8080/ready"
echo "   • Messages:   http://localhost:8080/v1/messages"
echo "   • Agents:     http://localhost:8080/v1/discovery/agents"
echo ""
echo "🧪 Test with these domains (DNS mock records enabled):"
echo "   • ✅ localhost - your local gateway (has mock TXT record)"
echo "   • ✅ test.local - test recipients (has mock TXT record)"
echo "   • ✅ dev.local - development testing (has mock TXT record)"
echo "   • ✅ example.com - test senders (has mock TXT record)"
echo "   • ⚠️  other domains - will fail discovery (as expected)"
echo ""
echo "📝 Example test command:"
echo "   curl -X POST http://localhost:8080/v1/messages \\"
echo "     -H 'Content-Type: application/json' \\"
echo "     -d '{\"sender\":\"test@localhost\",\"recipients\":[\"user@localhost\"],\"subject\":\"Test\",\"payload\":{\"msg\":\"Hello!\"}}'"
echo ""
echo "⚠️  Avoid using real domains (gmail.com, etc.) - they will fail DNS discovery"
echo ""
echo "==============================================="
echo ""

./build/agentry