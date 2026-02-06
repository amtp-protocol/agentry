#!/usr/bin/env bash
#
# Demo: Set up a 3-person team on AMTP and exchange messages.
#
# Prerequisites:
#   - Node.js 22+
#   - Agentry gateway running locally
#
# Usage:
#   # Terminal 1: Start Agentry
#   AMTP_TLS_ENABLED=false AMTP_SERVER_ADDRESS=:8080 AMTP_DOMAIN=localhost ./build/agentry
#
#   # Terminal 2: Run this script
#   ./examples/team-setup.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)/scripts"
AMTP="node $SCRIPT_DIR/amtp.mjs"

export AMTP_GATEWAY_URL="${AMTP_GATEWAY_URL:-http://localhost:8080}"
export AMTP_ADMIN_KEY="${AMTP_ADMIN_KEY:-admin}"

CONFIG_DIR=$(mktemp -d)
trap 'rm -rf "$CONFIG_DIR"' EXIT

echo "=== AMTP Team Setup Demo ==="
echo "Gateway: $AMTP_GATEWAY_URL"
echo ""

# Register three agents, saving each config separately
echo "--- Registering agents ---"
for name in alice bob carol; do
  HOME="$CONFIG_DIR/$name" mkdir -p "$CONFIG_DIR/$name"
  HOME="$CONFIG_DIR/$name" $AMTP setup --name "$name"
  echo ""
done

# Alice sends a message to Bob
echo "--- Alice sends message to Bob ---"
HOME="$CONFIG_DIR/alice" $AMTP send \
  --to "bob@localhost" \
  --subject "Project update" \
  --text "The build is green. Ready for review."
echo ""

# Alice sends a message to Carol
echo "--- Alice sends message to Carol ---"
HOME="$CONFIG_DIR/alice" $AMTP send \
  --to "carol@localhost" \
  --subject "Design review" \
  --payload '{"task":"review","priority":"high","files":["ui/dashboard.tsx"]}'
echo ""

# Bob checks inbox
echo "--- Bob checks inbox ---"
HOME="$CONFIG_DIR/bob" $AMTP inbox
echo ""

# Carol checks inbox
echo "--- Carol checks inbox ---"
HOME="$CONFIG_DIR/carol" $AMTP inbox
echo ""

# Discover all agents
echo "--- Discovering agents ---"
$AMTP discover
echo ""

# Show Alice's config
echo "--- Alice's identity ---"
HOME="$CONFIG_DIR/alice" $AMTP whoami
echo ""

echo "=== Demo complete ==="
