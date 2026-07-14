#!/usr/bin/env bash
set -Eeuo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
source "${SCRIPT_DIR}/common.sh"
start_log preflight

for command in docker curl tar sha256sum; do
  command -v "${command}" >/dev/null || { echo "Missing command: ${command}" >&2; exit 1; }
done

record_command docker version
record_command docker compose version
record_command docker info --format 'server={{.ServerVersion}} driver={{.Driver}} root={{.DockerRootDir}}'

[[ -f "${COMPOSE_FILE}" ]] || { echo "Compose file not found: ${COMPOSE_FILE}" >&2; exit 1; }
record_command compose config --quiet
record_command compose ps
record_command docker inspect "${APP_CONTAINER}" --format 'container={{.Name}} image={{.Image}} started={{.State.StartedAt}} status={{.State.Status}}'

for path in "${APP_DIR}" "${MEDIA_DIR}" "${LOG_DIR}" "${DATA_DIR}" "${BACKUP_ROOT}"; do
  if [[ -n "${path:-}" ]]; then
    [[ -e "${path}" ]] || { echo "Required path not found: ${path}" >&2; exit 1; }
    record_command df -h "${path}"
  fi
done

health_check "${HEALTHCHECK_URL}" 3 2
if [[ -n "${PUBLIC_STATUS_URL:-}" ]]; then
  health_check "${PUBLIC_STATUS_URL}" 3 2
fi

echo "Preflight passed. Review ${LOG_FILE} before backup."

