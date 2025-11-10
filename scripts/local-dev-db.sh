#!/bin/bash
# Local development configuration for AMTP Gateway with PostgreSQL
# This script sets up a local PostgreSQL database in Docker as storage and
# starts the gateway with mock DNS mode.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# PostgreSQL configuration
POSTGRES_CONTAINER_NAME="${POSTGRES_CONTAINER_NAME:-agentry-local-postgres}"
POSTGRES_IMAGE="${POSTGRES_IMAGE:-postgres:latest}"
POSTGRES_PORT="${POSTGRES_PORT:-5432}"
POSTGRES_USER="${POSTGRES_USER:-postgres}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-postgres}"
POSTGRES_DB="${POSTGRES_DB:-agentry}"

if ! command -v docker >/dev/null 2>&1; then
	echo "Docker is required to run this script." >&2
	exit 1
fi

CONTAINER_STARTED=false

cleanup() {
	if [[ "${CONTAINER_STARTED}" == "true" ]]; then
		docker stop "${POSTGRES_CONTAINER_NAME}" >/dev/null 2>&1 || true
	fi
}
trap cleanup EXIT

container_exists() {
	docker ps --format '{{.Names}}' | grep -Fx "${POSTGRES_CONTAINER_NAME}" >/dev/null 2>&1
}

container_defined() {
	docker ps -a --format '{{.Names}}' | grep -Fx "${POSTGRES_CONTAINER_NAME}" >/dev/null 2>&1
}

if container_exists; then
	echo "✅ PostgreSQL container '${POSTGRES_CONTAINER_NAME}' is already running."
    echo ""
else
	if container_defined; then
		echo "Removing existing PostgreSQL container '${POSTGRES_CONTAINER_NAME}'."
        echo ""
		docker rm -f "${POSTGRES_CONTAINER_NAME}" >/dev/null
	fi

	echo "🚀 Starting PostgreSQL container '${POSTGRES_CONTAINER_NAME}'."
	docker run -d \
		--name "${POSTGRES_CONTAINER_NAME}" \
		-p "${POSTGRES_PORT}:5432" \
		-e POSTGRES_USER="${POSTGRES_USER}" \
		-e POSTGRES_PASSWORD="${POSTGRES_PASSWORD}" \
		-e POSTGRES_DB="${POSTGRES_DB}" \
		-v "${PROJECT_ROOT}/deployment/db:/docker-entrypoint-initdb.d:ro" \
		"${POSTGRES_IMAGE}" >/dev/null

	CONTAINER_STARTED=true

	echo -n "⏳ Waiting for PostgreSQL to become ready"
	until docker exec "${POSTGRES_CONTAINER_NAME}" pg_isready -U "${POSTGRES_USER}" -d "${POSTGRES_DB}" >/dev/null 2>&1; do
		printf '.'
		sleep 2
	done
	echo " done."
fi

export AMTP_TLS_ENABLED=false
export AMTP_SERVER_ADDRESS=":8080"
export AMTP_DOMAIN="localhost"
export AMTP_LOG_LEVEL="debug"
export AMTP_LOG_FORMAT="text"
export AMTP_AUTH_REQUIRED=false
export AMTP_MESSAGE_VALIDATION_ENABLED=true
export AMTP_DNS_MOCK_MODE=true
export AMTP_DNS_ALLOW_HTTP=true

export AMTP_STORAGE_TYPE="database"
export AMTP_STORAGE_DATABASE_DRIVER="pgx"
export AMTP_STORAGE_DATABASE_CONNECTION_STRING="host=localhost port=${POSTGRES_PORT} user=${POSTGRES_USER} password=${POSTGRES_PASSWORD} dbname=${POSTGRES_DB} sslmode=disable"
export AMTP_STORAGE_DATABASE_MAX_CONNECTIONS=25
export AMTP_STORAGE_DATABASE_MAX_IDLE_TIME=300

export AGENTRY_BINARY="${PROJECT_ROOT}/build/agentry"


print_info() {
    echo ""
    echo "==============================================="
    echo "🚀 Starting Agentry with local PostgreSQL"
    echo "==============================================="
    echo ""
    echo "📦 Database:"
    echo "  • Container: ${POSTGRES_CONTAINER_NAME}"
    echo "  • Image:     ${POSTGRES_IMAGE}"
    echo "  • Host:      localhost:${POSTGRES_PORT}"
    echo "  • Database:  ${POSTGRES_DB}"
    echo "  • User:      ${POSTGRES_USER}"
    echo ""
    echo "📍 Server Configuration:"
    echo "  • Address:   http://localhost:8080"
    echo "  • Domain:    localhost"
    echo "  • TLS:       disabled"
    echo "  • Auth:      disabled"
    echo "  • DNS:       mock mode enabled"
    echo "  • Storage:   PostgreSQL (pgx driver)"
    echo ""
    echo "🔗 Available Endpoints:"
    echo "  • Health:     http://localhost:8080/health"
    echo "  • Ready:      http://localhost:8080/ready"
    echo "  • Messages:   http://localhost:8080/v1/messages"
    echo "  • Agents:     http://localhost:8080/v1/discovery/agents"
    echo ""
    echo "📝 Connection string used:"
    echo "  • ${AMTP_STORAGE_DATABASE_CONNECTION_STRING}"
    echo ""
    echo "==============================================="
    echo ""
}

print_info

if [[ ! -x "${AGENTRY_BINARY}" ]]; then
	echo "Agentry binary not found at ${AGENTRY_BINARY}. Run 'make build' first." >&2
	exit 1
fi

"${AGENTRY_BINARY}"
