#!/bin/bash

# Signed domain-simulation E2E (Docker).
#
# Generates a signing key per domain with `agentry-admin keygen`, writes a
# dnsmasq config publishing both the _amtp capability records and the
# k1._amtpkey.<domain> signing-key records, starts the signed-simulation
# compose stack, and runs the cross-domain signature assertions:
#
#   1. A -> B signed delivery is stored at B with sender_verification=verified.
#   2. Forged unsigned A-sender at B (reject policy) -> 403 SIGNATURE_REJECTED.
#   3. Forged unsigned A-sender at Partner (flag policy) -> accepted, recorded unsigned.
#   4. Tampered signed body at B -> 403.
#   5. DNS signing key unavailable at B -> 503.
#
# Keys are generated into /tmp at runtime and never committed.
#
# Usage: scripts/test-signed-simulation.sh [start|stop]
#   start (default) - generate keys, start stack, run assertions, tear down
#   stop            - tear the stack down and clean temp files

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
COMPOSE_FILE="$PROJECT_ROOT/docker/docker-compose.signed-simulation.yml"

source "$SCRIPT_DIR/utils.sh"

KEY_DIR="/tmp/amtp-signed-keys"
DNS_DIR="/tmp/amtp-signed-dns"
DOMAINS=(company-a.local company-b.local partner.local)
ADMIN_KEY="signed-sim-admin-key"

GATEWAY_A="http://localhost:8180"
GATEWAY_B="http://localhost:8181"
GATEWAY_P="http://localhost:8182"

compose() {
    docker compose -f "$COMPOSE_FILE" "$@"
}

stop_stack() {
    log_step "Tearing down signed simulation stack"
    compose down -v --remove-orphans >/dev/null 2>&1 || true
    rm -rf "$KEY_DIR" "$DNS_DIR"
    log_success "Stack removed"
}

# generate_keys creates one ES256 key per domain plus the shared admin key
# file, and emits the dnsmasq config with capability + signing-key records.
generate_keys() {
    log_step "Generating per-domain signing keys"

    rm -rf "$KEY_DIR" "$DNS_DIR"
    mkdir -p "$KEY_DIR" "$DNS_DIR"

    printf '%s' "$ADMIN_KEY" > "$KEY_DIR/admin.key"

    local dns_conf=""
    dns_conf+="no-daemon\n"
    dns_conf+="log-queries\n"
    dns_conf+="server=8.8.8.8\n"

    for domain in "${DOMAINS[@]}"; do
        local key_file="$KEY_DIR/$domain.pem"
        local out
        out=$(cd "$PROJECT_ROOT" && go run ./cmd/agentry-admin keygen \
            --domain "$domain" --key-id k1 --out "$key_file") || {
            log_error "keygen failed for $domain"
            exit 1
        }
        # The TXT value is the line starting with "TXT: ".
        local txt
        txt=$(printf '%s\n' "$out" | sed -n 's/^TXT: //p')
        if [ -z "$txt" ]; then
            log_error "keygen produced no TXT record for $domain"
            exit 1
        fi
        dns_conf+="txt-record=_amtp.$domain,\"v=amtp1;gateway=http://$domain:8080;features=agent-discovery\"\n"
        dns_conf+="txt-record=k1._amtpkey.$domain,\"$txt\"\n"
        log_info "generated key for $domain (owner k1._amtpkey.$domain)"
    done

    printf "$dns_conf" > "$DNS_DIR/dnsmasq.conf"
    chmod 600 "$KEY_DIR"/*.pem "$KEY_DIR"/admin.key
    log_success "Keys and dnsmasq config written to $KEY_DIR and $DNS_DIR"
}

wait_for_gateways() {
    log_step "Waiting for gateways to become healthy"
    local deadline=$((SECONDS + 180))
    for port in 8180 8181 8182; do
        until curl -sf "http://localhost:$port/health" >/dev/null 2>&1; do
            if [ "$SECONDS" -ge "$deadline" ]; then
                log_error "gateway on port $port did not become healthy"
                exit 1
            fi
            sleep 2
        done
    done
    log_success "All gateways healthy"
}

# register_agent registers an agent on a gateway and prints its API key.
register_agent() {
    local base_url="$1" address="$2"
    local resp
    resp=$(curl -sf -X POST "$base_url/v1/admin/agents" \
        -H "Content-Type: application/json" \
        -H "X-Admin-Key: $ADMIN_KEY" \
        -d "{\"address\": \"$address\", \"delivery_mode\": \"pull\"}") || {
        log_error "agent registration failed for $address"
        exit 1
    }
    printf '%s' "$resp" | jq -r '.agent.api_key'
}

# send_signed_from_a signs a single-recipient body with A's key and posts it
# to the target gateway. Prints the HTTP status code. With the fourth argument
# set to "tamper", the payload is modified after signing so the signature no
# longer matches the body.
send_signed_from_a() {
    local target_url="$1" recipient="$2" payload="$3" mode="${4:-}"
    local key_file="$KEY_DIR/company-a.local.pem"
    python3 - "$key_file" "$target_url" "$recipient" "$payload" "$mode" <<'PYEOF'
import base64, hashlib, json, os, sys, time, uuid
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import ec
from cryptography.hazmat.primitives.asymmetric.utils import decode_dss_signature

key_file, target_url, recipient, payload, mode = sys.argv[1:6]

with open(key_file, "rb") as f:
    key = serialization.load_pem_private_key(f.read(), password=None)


def uuid7():
    """UUIDv7: 48-bit big-endian unix-millis prefix + random remainder.

    The gateway validates message_id with uuid.IsValidV7, so the version
    nibble must be 7 and the variant nibble must be 8/9/a/b.
    """
    unix_ms = int(time.time() * 1000)
    rand = os.urandom(10)
    b = unix_ms.to_bytes(6, "big") + bytes([0x70]) + rand[:1] + bytes([0x80 | (rand[1] & 0x3F)]) + rand[2:]
    h = b.hex()
    return f"{h[0:8]}-{h[8:12]}-{h[12:16]}-{h[16:20]}-{h[20:32]}"


body = {
    "version": "1.0",
    "message_id": uuid7(),
    "idempotency_key": str(uuid.uuid4()),
    "timestamp": "2026-01-02T03:04:05Z",
    "sender": "sender@company-a.local",
    "recipients": [recipient],
    "subject": "signed-e2e",
    "payload": json.loads(payload),
}

# RFC 8785 canonicalization: sorted keys, no whitespace, minimal number
# serialization. The fixed field set here contains only strings and a small
# integer-free payload, so json.dumps with sort_keys and separators matches
# JCS for this input.
canonical = json.dumps(body, sort_keys=True, separators=(",", ":"), ensure_ascii=False)
digest = hashlib.sha256(canonical.encode("utf-8")).digest()
sig = key.sign(digest, ec.ECDSA(hashes.SHA256()))
r, s = decode_dss_signature(sig)
raw = r.to_bytes(32, "big") + s.to_bytes(32, "big")
b64 = base64.urlsafe_b64encode(raw).rstrip(b"=").decode("ascii")

body["signature"] = {"algorithm": "ES256", "keyid": "k1", "value": b64}

if mode == "tamper":
    # Modify the signed body after signing: the signature no longer matches.
    body["payload"]["tampered"] = True

import urllib.request
req = urllib.request.Request(
    target_url + "/v1/messages",
    data=json.dumps(body).encode("utf-8"),
    headers={"Content-Type": "application/json"},
    method="POST",
)
try:
    with urllib.request.urlopen(req) as resp:
        print(resp.status)
except urllib.error.HTTPError as e:
    print(e.code)
PYEOF
}

# assert_eq fails the script when the two arguments differ.
assert_eq() {
    local what="$1" got="$2" want="$3"
    if [ "$got" = "$want" ]; then
        log_success "$what: $got"
    else
        log_error "$what: got $got, want $want"
        exit 1
    fi
}

# poll_sender_verification polls a gateway as the given agent until a message
# from the sender appears, then prints its sender_verification.result.
poll_sender_verification() {
    local base_url="$1" api_key="$2" sender="$3"
    local verification=""
    local deadline=$((SECONDS + 30))
    while [ $SECONDS -lt $deadline ]; do
        local list msg_id
        list=$(curl -sf "$base_url/v1/messages?sender=$sender&limit=5" \
            -H "Authorization: Bearer $api_key") || { sleep 1; continue; }
        msg_id=$(printf '%s' "$list" | jq -r '.messages[0].message_id // empty')
        if [ -n "$msg_id" ]; then
            verification=$(curl -sf "$base_url/v1/messages/$msg_id/status" \
                -H "Authorization: Bearer $api_key" \
                | jq -r '.sender_verification.result // empty')
            [ -n "$verification" ] && break
        fi
        sleep 1
    done
    printf '%s' "$verification"
}

run_assertions() {
    log_step "Registering agents"
    local key_a key_b key_p
    key_a=$(register_agent "$GATEWAY_A" "sender")
    key_b=$(register_agent "$GATEWAY_B" "receiver")
    key_p=$(register_agent "$GATEWAY_P" "receiver")
    log_info "agents registered on all gateways"

    # --- 1. A -> B signed delivery: B records verified --------------------
    log_step "A -> B signed delivery must be recorded as verified"
    local status
    status=$(curl -sf -X POST "$GATEWAY_A/v1/messages" \
        -H "Content-Type: application/json" \
        -H "Authorization: Bearer $key_a" \
        -d '{
            "sender": "sender@company-a.local",
            "recipients": ["receiver@company-b.local"],
            "subject": "cross-domain",
            "payload": {"hello": "world"}
        }' | jq -r '.status')
    assert_eq "A accepted the message" "$status" "delivered"

    local verification
    verification=$(poll_sender_verification "$GATEWAY_B" "$key_b" "sender@company-a.local")
    assert_eq "B sender_verification.result" "$verification" "verified"

    # --- 2. Forged unsigned at B (reject) -> 403 --------------------------
    log_step "Forged unsigned A-sender at B (reject) must be refused"
    local code
    code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$GATEWAY_B/v1/messages" \
        -H "Content-Type: application/json" \
        -d '{
            "sender": "sender@company-a.local",
            "recipients": ["receiver@company-b.local"],
            "payload": {"forged": true}
        }')
    assert_eq "forged unsigned at B HTTP status" "$code" "403"

    # --- 3. Forged unsigned at Partner (flag) -> accepted, recorded -------
    log_step "Forged unsigned A-sender at Partner (flag) must be accepted and recorded"
    code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$GATEWAY_P/v1/messages" \
        -H "Content-Type: application/json" \
        -d '{
            "sender": "sender@company-a.local",
            "recipients": ["receiver@partner.local"],
            "payload": {"forged": true}
        }')
    assert_eq "forged unsigned at Partner HTTP status" "$code" "200"

    verification=$(poll_sender_verification "$GATEWAY_P" "$key_p" "sender@company-a.local")
    assert_eq "Partner sender_verification.result" "$verification" "unsigned"

    # --- 4. Tampered signed body at B -> 403 ------------------------------
    log_step "Tampered signed delivery at B must be refused"
    code=$(send_signed_from_a "$GATEWAY_B" "receiver@company-b.local" '{"hello": "world"}' tamper)
    assert_eq "tampered delivery HTTP status" "$code" "403"

    # --- 5. DNS signing key unavailable at B -> 503 ------------------------
    log_step "Signing key removed from DNS must yield 503"
    # Remove company-a's signing-key record from dnsmasq and restart it. The
    # gateways run with AMTP_DNS_CACHE_TTL=2s, so the cached record expires
    # quickly and the next verification cannot resolve the key. The config is
    # rewritten in place (not sed -i, which would replace the file and break
    # the bind mount inside the container).
    grep -v 'k1._amtpkey.company-a.local' "$DNS_DIR/dnsmasq.conf" > "$DNS_DIR/dnsmasq.conf.tmp"
    cat "$DNS_DIR/dnsmasq.conf.tmp" > "$DNS_DIR/dnsmasq.conf"
    rm -f "$DNS_DIR/dnsmasq.conf.tmp"
    compose restart dns-server >/dev/null 2>&1
    sleep 3

    code=$(send_signed_from_a "$GATEWAY_B" "receiver@company-b.local" '{"hello": "world"}')
    assert_eq "key unavailable HTTP status" "$code" "503"

    log_success "All signed-simulation assertions passed"
}

main() {
    local action="${1:-start}"
    case "$action" in
        stop)
            stop_stack
            return
            ;;
        start) ;;
        *)
            echo "usage: $0 [start|stop]" >&2
            exit 2
            ;;
    esac

    generate_keys

    log_step "Starting signed simulation stack"
    compose up -d --build >/dev/null
    trap stop_stack EXIT

    wait_for_gateways
    run_assertions
}

main "$@"
