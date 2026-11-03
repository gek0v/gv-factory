#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: 2026 gv-factory contributors

set -euo pipefail

# Determine script and repository root directories
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${REPO_ROOT}"

# Formatted console output helpers
INFO="[INFO]"
WARN="[WARN]"
ERROR="[ERROR]"
SUCCESS="[SUCCESS]"
BOLD=""
RESET=""

if [ -t 1 ]; then
  GREEN="\033[0;32m"
  YELLOW="\033[1;33m"
  RED="\033[0;31m"
  BLUE="\033[0;34m"
  BOLD="\033[1m"
  RESET="\033[0m"
  INFO="${BLUE}[INFO]${RESET}"
  WARN="${YELLOW}[WARN]${RESET}"
  ERROR="${RED}[ERROR]${RESET}"
  SUCCESS="${GREEN}[SUCCESS]${RESET}"
fi

ADR_PATH="docs/adr"
SERVER_URL="${OV_SERVER_URL:-http://localhost:8080}"
DRY_RUN=false
USE_API=false

usage() {
  cat <<EOF
Uso: $0 [opciones]

Sincroniza e indexa los Architecture Decision Records (ADRs) en la base de datos del Cerebro.

Opciones:
  -p, --path <ruta>      Ruta al directorio de ADRs (por defecto: docs/adr)
  -s, --server <url>     URL de ov-server (por defecto: http://localhost:8080 o \$OV_SERVER_URL)
  -d, --dry-run          Ejecuta la sincronización en modo simulación (sin escribir en BD)
  --api                  Fuerza el uso directo del endpoint HTTP /api/v1/brain/sync/adrs
  -h, --help             Muestra este mensaje de ayuda

Ejemplos:
  $0
  $0 --path docs/adr --dry-run
  $0 --server http://127.0.0.1:8080 --api
EOF
  exit 0
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
  case "$1" in
    -p|--path)
      ADR_PATH="$2"
      shift 2
      ;;
    -s|--server)
      SERVER_URL="$2"
      shift 2
      ;;
    -d|--dry-run)
      DRY_RUN=true
      shift
      ;;
    --api)
      USE_API=true
      shift
      ;;
    -h|--help)
      usage
      ;;
    *)
      echo -e "${ERROR} Parámetro desconocido: $1" >&2
      usage
      ;;
  esac
done

echo -e "${BOLD}==================================================================${RESET}"
echo -e "${BOLD}        GV-Factory: Sincronización de ADRs con el Cerebro        ${RESET}"
echo -e "${BOLD}==================================================================${RESET}"
echo -e "${INFO} Directorio ADRs:  ${ADR_PATH}"
echo -e "${INFO} Servidor API:     ${SERVER_URL}"
echo -e "${INFO} Modo Simulación:  ${DRY_RUN}"

if [ ! -d "${ADR_PATH}" ]; then
  echo -e "${ERROR} Directorio no encontrado: ${ADR_PATH}" >&2
  exit 1
fi

# Strategy 1: If --api is requested or server is reachable via curl
if [ "${USE_API}" = true ] || curl -s --connect-timeout 2 "${SERVER_URL}/health" >/dev/null 2>&1; then
  echo -e "${INFO} Invocando API remota: ${SERVER_URL}/api/v1/brain/sync/adrs..."

  PAYLOAD=$(printf '{"path":"%s"}' "${ADR_PATH}")
  HTTP_RESPONSE=$(curl -s -w "\n%{http_code}" -X POST "${SERVER_URL}/api/v1/brain/sync/adrs" \
    -H "Content-Type: application/json" \
    -d "${PAYLOAD}" 2>/dev/null || echo -e "\n000")

  HTTP_STATUS=$(echo "${HTTP_RESPONSE}" | tail -n1)
  HTTP_BODY=$(echo "${HTTP_RESPONSE}" | sed '$d')

  if [ "${HTTP_STATUS}" -eq 200 ]; then
    echo -e "${SUCCESS} Sincronización exitosa a través de la API."
    echo -e "${INFO} Respuesta del servidor:\n${HTTP_BODY}"
    exit 0
  else
    echo -e "${WARN} El endpoint de la API retornó código HTTP ${HTTP_STATUS}: ${HTTP_BODY}"
    echo -e "${INFO} Intentando alternativa mediante 'ov-cli'..."
  fi
fi

# Strategy 2: Use ov-cli (binary in PATH, bin/ov-cli or go run)
CLI_CMD=""
if command -v ov-cli >/dev/null 2>&1; then
  CLI_CMD="ov-cli"
elif [ -x "./bin/ov-cli" ]; then
  CLI_CMD="./bin/ov-cli"
elif [ -f "./cmd/ov-cli/main.go" ]; then
  CLI_CMD="go run ./cmd/ov-cli"
fi

if [ -n "${CLI_CMD}" ]; then
  echo -e "${INFO} Ejecutando comando CLI: ${CLI_CMD} memory sync-adrs..."

  CLI_ARGS=(memory sync-adrs "${ADR_PATH}")
  if [ "${DRY_RUN}" = true ]; then
    CLI_ARGS+=(-dry-run)
  fi

  if ${CLI_CMD} "${CLI_ARGS[@]}"; then
    echo -e "${SUCCESS} Sincronización de ADRs completada exitosamente."
    exit 0
  else
    echo -e "${ERROR} Falló la sincronización con ${CLI_CMD}." >&2
    exit 1
  fi
fi

echo -e "${ERROR} No se pudo conectar a ov-server en ${SERVER_URL} y no se encontró ov-cli disponible." >&2
exit 1
