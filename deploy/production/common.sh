#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="${ENV_FILE:-${SCRIPT_DIR}/release.env}"

if [[ ! -f "${ENV_FILE}" ]]; then
  echo "Missing ${ENV_FILE}. Copy release.env.example and fill in server values." >&2
  exit 1
fi

# shellcheck disable=SC1090
source "${ENV_FILE}"

if [[ "${APP_SERVICE:-}" != "new-api" ]]; then
  echo "compose.release.yml currently requires APP_SERVICE=new-api" >&2
  exit 1
fi

require_var() {
  local name="$1"
  if [[ -z "${!name:-}" ]]; then
    echo "Required setting ${name} is empty" >&2
    exit 1
  fi
}

for required in RELEASE_ID APP_DIR COMPOSE_FILE APP_SERVICE APP_CONTAINER BACKUP_ROOT HEALTHCHECK_URL; do
  require_var "${required}"
done

RELEASE_DIR="${BACKUP_ROOT}/${RELEASE_ID}"
RUN_LOG_DIR="${RELEASE_DIR}/run-logs"
mkdir -p "${RUN_LOG_DIR}"

start_log() {
  local operation="$1"
  local timestamp
  timestamp="$(date +%Y%m%d-%H%M%S)"
  LOG_FILE="${RUN_LOG_DIR}/${timestamp}-${operation}.log"
  exec > >(tee -a "${LOG_FILE}") 2>&1
  echo "[$(date --iso-8601=seconds)] operation=${operation} release=${RELEASE_ID}"
}

compose() {
  docker compose -f "${COMPOSE_FILE}" "$@"
}

release_compose() {
  NEW_API_IMAGE="$1" docker compose \
    -f "${COMPOSE_FILE}" \
    -f "${SCRIPT_DIR}/compose.release.yml" \
    "${@:2}"
}

record_command() {
  echo "[$(date --iso-8601=seconds)] $*"
  "$@"
}

health_check() {
  local url="$1"
  local attempts="${2:-20}"
  local delay="${3:-3}"
  local body
  for ((i = 1; i <= attempts; i++)); do
    if body="$(curl --fail --silent --show-error --max-time 10 "${url}" 2>/dev/null)" \
      && grep -Eq '"success"[[:space:]]*:[[:space:]]*true' <<<"${body}"; then
      echo "Health check passed: ${url}"
      return 0
    fi
    echo "Health check ${i}/${attempts} not ready"
    sleep "${delay}"
  done
  echo "Health check failed: ${url}" >&2
  return 1
}

write_manifest_value() {
  local key="$1"
  local value="$2"
  printf '%s=%q\n' "${key}" "${value}" >>"${RELEASE_DIR}/manifest.env"
}

write_state_value() {
  local key="$1"
  local value="$2"
  printf '%s=%q\n' "${key}" "${value}" >>"${RELEASE_DIR}/release-state.env"
}
