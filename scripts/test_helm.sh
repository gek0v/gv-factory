#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: 2026 gv-factory contributors

set -e
set -o pipefail

# Determine repository root
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${REPO_ROOT}"

CHART_DIR="${REPO_ROOT}/deployments/helm/gv-factory"

# Terminal formatting
if [ -t 1 ]; then
  GREEN="\033[0;32m"
  YELLOW="\033[1;33m"
  RED="\033[0;31m"
  BLUE="\033[0;34m"
  BOLD="\033[1m"
  RESET="\033[0m"
else
  GREEN=""
  YELLOW=""
  RED=""
  BLUE=""
  BOLD=""
  RESET=""
fi

log_info() {
  echo -e "${BLUE}[INFO]${RESET} $*"
}

log_success() {
  echo -e "${GREEN}[SUCCESS]${RESET} $*"
}

log_warn() {
  echo -e "${YELLOW}[WARN]${RESET} $*"
}

log_error() {
  echo -e "${RED}[ERROR]${RESET} $*" >&2
}

echo -e "${BOLD}================================================================${RESET}"
echo -e "${BOLD}gv-factory Helm Chart & Template Validation Suite${RESET}"
echo -e "${BOLD}================================================================${RESET}"

# 1. Check prerequisite tools
log_info "Verifying prerequisites..."
if ! command -v helm &>/dev/null; then
  log_error "Helm binary not found in PATH. Please install Helm v3.10+."
  exit 1
fi
log_success "Helm binary detected: $(helm version --short)"

if [ ! -d "${CHART_DIR}" ]; then
  log_error "Chart directory not found at: ${CHART_DIR}"
  exit 1
fi

# 2. Helm Lint
log_info "Running 'helm lint' on chart ${CHART_DIR}..."
if helm lint "${CHART_DIR}" --strict; then
  log_success "Helm lint passed with 0 errors!"
else
  log_error "Helm lint failed!"
  exit 1
fi

# 3. YAML Syntax & Structural Verification using Python PyYAML
validate_rendered_yaml() {
  local test_name="$1"
  shift
  local helm_flags=("$@")

  log_info "Testing render profile: ${BOLD}${test_name}${RESET}..."

  local rendered_output
  if ! rendered_output=$(helm template test-release "${CHART_DIR}" "${helm_flags[@]}"); then
    log_error "Failed to render templates for profile: ${test_name}"
    return 1
  fi

  # Validate YAML syntax with Python if available
  if command -v python &>/dev/null; then
    local py_val
    py_val=$(python -c '
import sys, yaml

try:
    content = sys.stdin.read()
    docs = [d for d in yaml.safe_load_all(content) if d]
    if not docs:
        print("ERROR: Rendered output contained no valid documents")
        sys.exit(1)
    for doc in docs:
        if not isinstance(doc, dict):
            continue
        kind = doc.get("kind")
        name = doc.get("metadata", {}).get("name")
        if not kind or not name:
            print(f"ERROR: Document missing kind or name: {doc}")
            sys.exit(1)
    print(f"OK:{len(docs)}")
except Exception as e:
    print(f"ERROR: YAML parse error: {e}")
    sys.exit(1)
' <<< "${rendered_output}")

    if [[ "${py_val}" == OK:* ]]; then
      local count="${py_val#OK:}"
      log_success "Profile '${test_name}' parsed cleanly (${count} Kubernetes resources)."
    else
      log_error "YAML validation failed for profile '${test_name}': ${py_val}"
      return 1
    fi
  else
    log_warn "Python not found; skipping deep PyYAML document validation for '${test_name}'."
  fi
}

# 4. Test multiple configurations / value overrides
log_info "Executing template permutation tests..."

# Test Default Values
validate_rendered_yaml "Default Values"

# Test Profile A: Ollama Provider + Traefik Ingress
validate_rendered_yaml "Ollama Provider + Traefik Ingress" \
  --set ingress.className="traefik" \
  --set llm.provider="ollama" \
  --set llm.ollama.baseUrl="http://ollama-service:11434"

# Test Profile B: vLLM Provider + Docker Socket Access Enabled
validate_rendered_yaml "vLLM Provider + Docker Socket Access" \
  --set llm.provider="vllm" \
  --set llm.vllm.baseUrl="http://vllm:8000/v1" \
  --set worker.dockerSocket.enabled=true

# Test Profile C: External PostgreSQL & NATS
validate_rendered_yaml "External PostgreSQL & NATS" \
  --set postgres.external.enabled=true \
  --set postgres.external.url="postgres://user:pass@db.external:5432/ov" \
  --set nats.external.enabled=true \
  --set nats.external.url="nats://nats.external:4222"

# Test Profile D: External Secret (existingSecret)
validate_rendered_yaml "Existing Secret Integration" \
  --set secret.existingSecret="my-precreated-secret"

# Test Profile E: Worker Autoscaling Enabled
validate_rendered_yaml "Worker Autoscaling Enabled" \
  --set worker.autoscaling.enabled=true \
  --set worker.autoscaling.minReplicas=3 \
  --set worker.autoscaling.maxReplicas=12

# 5. REUSE Compliance check
log_info "Verifying REUSE license compliance..."
if command -v reuse &>/dev/null; then
  if reuse lint > /dev/null 2>&1; then
    log_success "REUSE compliance verified (all files have valid SPDX headers)!"
  else
    log_error "REUSE compliance check failed!"
    reuse lint
    exit 1
  fi
else
  log_warn "reuse CLI not detected; skipping license check."
fi

echo -e "${BOLD}================================================================${RESET}"
echo -e "${GREEN}${BOLD}ALL HELM CHART VALIDATIONS PASSED SUCCESSFULLY!${RESET}"
echo -e "${BOLD}================================================================${RESET}"
exit 0
