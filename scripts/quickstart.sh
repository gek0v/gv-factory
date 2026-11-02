#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: 2026 gv-factory contributors

set -e
set -o pipefail

# Determine script and repository root directories
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${REPO_ROOT}"

# Formatted console output helpers
INFO="[INFO]"
WARN="[WARN]"
ERROR="[ERROR]"
SUCCESS="[SUCCESS]"

if [ -t 1 ]; then
  GREEN="\033[0;32m"
  YELLOW="\033[1;33m"
  RED="\033[0;31m"
  BLUE="\033[0;34m"
  RESET="\033[0m"
  INFO="${BLUE}[INFO]${RESET}"
  WARN="${YELLOW}[WARN]${RESET}"
  ERROR="${RED}[ERROR]${RESET}"
  SUCCESS="${GREEN}[SUCCESS]${RESET}"
fi

echo "=================================================================="
echo "         GV-Factory Universal Deployment Quickstart"
echo "=================================================================="

# Check for --dry-run / --check-only flags
DRY_RUN=false
PASSTHROUGH_ARGS=()

for arg in "$@"; do
  case "$arg" in
    --dry-run|--check-only)
      DRY_RUN=true
      ;;
    *)
      PASSTHROUGH_ARGS+=("$arg")
      ;;
  esac
done

# ------------------------------------------------------------------------------
# 1. Verify Docker Engine & Docker Compose
# ------------------------------------------------------------------------------
echo -e "${INFO} Verifying Docker engine availability..."

if ! command -v docker >/dev/null 2>&1; then
  echo -e "${ERROR} Docker CLI is not installed or not in PATH."
  echo "Please install Docker: https://docs.docker.com/get-docker/"
  exit 1
fi

if ! docker info >/dev/null 2>&1; then
  echo -e "${ERROR} Docker daemon is not reachable. Is Docker running?"
  echo "Please start the Docker daemon and rerun this script."
  exit 1
fi

COMPOSE_CMD=""
if docker compose version >/dev/null 2>&1; then
  COMPOSE_CMD="docker compose"
elif command -v docker-compose >/dev/null 2>&1; then
  COMPOSE_CMD="docker-compose"
else
  echo -e "${ERROR} Docker Compose plugin or standalone binary not found."
  echo "Please install Docker Compose: https://docs.docker.com/compose/install/"
  exit 1
fi

echo -e "${SUCCESS} Docker engine and Compose (${COMPOSE_CMD}) verified."

# ------------------------------------------------------------------------------
# 2. Environment Configuration & AUTH_SECRET_KEY Generation
# ------------------------------------------------------------------------------
echo -e "${INFO} Configuring deployment environment..."

ENV_FILE="${REPO_ROOT}/deployments/.env"
ENV_EXAMPLE="${REPO_ROOT}/deployments/.env.example"

if [ ! -f "${ENV_FILE}" ]; then
  if [ -f "${ENV_EXAMPLE}" ]; then
    echo -e "${INFO} Creating ${ENV_FILE} from .env.example..."
    cp "${ENV_EXAMPLE}" "${ENV_FILE}"
  else
    echo -e "${INFO} Creating new empty ${ENV_FILE}..."
    touch "${ENV_FILE}"
  fi
fi

generate_secret_key() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 32
  elif command -v python3 >/dev/null 2>&1; then
    python3 -c "import secrets; print(secrets.token_hex(32))"
  elif command -v python >/dev/null 2>&1; then
    python -c "import secrets; print(secrets.token_hex(32))"
  elif [ -r /dev/urandom ]; then
    od -vN 32 -An -tx1 /dev/urandom | tr -d ' \n'
  else
    date +%s%N | sha256sum | awk '{print $1}'
  fi
}

EXISTING_KEY=""
if [ -f "${ENV_FILE}" ]; then
  EXISTING_KEY=$(grep -E '^AUTH_SECRET_KEY=' "${ENV_FILE}" | cut -d '=' -f2- || true)
fi

if [ -z "${EXISTING_KEY}" ] || [ "${EXISTING_KEY}" = "dev_auth_secret_key_change_in_production" ]; then
  NEW_AUTH_KEY=$(generate_secret_key)
  echo -e "${INFO} Generating cryptographically secure AUTH_SECRET_KEY..."
  if grep -q '^AUTH_SECRET_KEY=' "${ENV_FILE}" 2>/dev/null; then
    # Replace existing placeholder key
    if [[ "$OSTYPE" == "darwin"* ]]; then
      sed -i '' "s|^AUTH_SECRET_KEY=.*|AUTH_SECRET_KEY=${NEW_AUTH_KEY}|" "${ENV_FILE}"
    else
      sed -i "s|^AUTH_SECRET_KEY=.*|AUTH_SECRET_KEY=${NEW_AUTH_KEY}|" "${ENV_FILE}"
    fi
  else
    echo "AUTH_SECRET_KEY=${NEW_AUTH_KEY}" >> "${ENV_FILE}"
  fi
  export AUTH_SECRET_KEY="${NEW_AUTH_KEY}"
  echo -e "${SUCCESS} AUTH_SECRET_KEY generated and saved to ${ENV_FILE}."
else
  export AUTH_SECRET_KEY="${EXISTING_KEY}"
  echo -e "${SUCCESS} Existing AUTH_SECRET_KEY preserved."
fi

# ------------------------------------------------------------------------------
# 3. Port Conflict Detection
# ------------------------------------------------------------------------------
echo -e "${INFO} Checking port availability..."

PORTS_TO_CHECK=(
  "5432:PostgreSQL (The Brain)"
  "4222:NATS JetStream"
  "7077:Hatchet Durable Engine"
  "8071:Svix Outbound Webhooks"
  "8080:ov-server API"
  "8081:ov-worker Health"
  "3000:ov-web Visual Dashboard"
)

is_port_in_use() {
  local port="$1"
  if command -v nc >/dev/null 2>&1; then
    nc -z 127.0.0.1 "${port}" >/dev/null 2>&1
  elif command -v lsof >/dev/null 2>&1; then
    lsof -i :"${port}" -sTCP:LISTEN >/dev/null 2>&1
  elif (echo >/dev/tcp/127.0.0.1/"${port}") >/dev/null 2>&1; then
    return 0
  else
    return 1
  fi
}

CONFLICTS_FOUND=false
for entry in "${PORTS_TO_CHECK[@]}"; do
  PORT="${entry%%:*}"
  NAME="${entry#*:}"
  if is_port_in_use "${PORT}"; then
    echo -e "${WARN} Port ${PORT} (${NAME}) is currently occupied by an active process."
    CONFLICTS_FOUND=true
  fi
done

if [ "${CONFLICTS_FOUND}" = true ]; then
  echo -e "${WARN} Some ports are already bound. If an existing GV-Factory stack is running, it will be updated."
else
  echo -e "${SUCCESS} All required network ports are available."
fi

# ------------------------------------------------------------------------------
# 4. Launch Docker Compose Stack
# ------------------------------------------------------------------------------
COMPOSE_FILE="${REPO_ROOT}/deployments/docker-compose.yml"

if [ "${DRY_RUN}" = true ]; then
  echo -e "${SUCCESS} Dry run check completed successfully. Compose command would be:"
  echo "  ${COMPOSE_CMD} -f ${COMPOSE_FILE} up -d ${PASSTHROUGH_ARGS[*]}"
  exit 0
fi

echo -e "${INFO} Starting GV-Factory multi-container stack via Docker Compose..."
${COMPOSE_CMD} -f "${COMPOSE_FILE}" up -d "${PASSTHROUGH_ARGS[@]}"

echo ""
echo "=================================================================="
echo -e "${SUCCESS} GV-Factory stack started successfully!"
echo "=================================================================="
echo "  - Visual DAG Dashboard:   http://localhost:3000"
echo "  - REST API & Gateways:    http://localhost:8080"
echo "  - Server Health Status:   http://localhost:8080/healthz"
echo "  - Worker Health Status:   http://localhost:8081/healthz"
echo "  - NATS Monitor Interface: http://localhost:8222"
echo "=================================================================="
echo "  Useful commands:"
echo "    Check status:  ${COMPOSE_CMD} -f ${COMPOSE_FILE} ps"
echo "    Tail logs:     ${COMPOSE_CMD} -f ${COMPOSE_FILE} logs -f"
echo "    Stop stack:    ${COMPOSE_CMD} -f ${COMPOSE_FILE} down"
echo "=================================================================="
