#!/usr/bin/env bash
set -Eeuo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
source "${SCRIPT_DIR}/common.sh"
start_log collect-logs

bundle_dir="${RELEASE_DIR}/diagnostics/$(date +%Y%m%d-%H%M%S)"
mkdir -p "${bundle_dir}"

docker compose -f "${COMPOSE_FILE}" ps --all >"${bundle_dir}/compose-ps.txt" 2>&1 || true
docker compose -f "${COMPOSE_FILE}" logs --no-color --timestamps --since "${LOG_SINCE:-2h}" >"${bundle_dir}/compose.log" 2>&1 || true
docker inspect "${APP_CONTAINER}" --format '{
  "name": {{json .Name}},
  "image_id": {{json .Image}},
  "image_ref": {{json .Config.Image}},
  "created": {{json .Created}},
  "state": {{json .State}},
  "mounts": {{json .Mounts}},
  "ports": {{json .NetworkSettings.Ports}}
}' >"${bundle_dir}/container-summary.json" 2>&1 || true
docker stats --no-stream >"${bundle_dir}/docker-stats.txt" 2>&1 || true
df -h >"${bundle_dir}/disk.txt" 2>&1 || true
free -h >"${bundle_dir}/memory.txt" 2>&1 || true
curl --silent --show-error --max-time 10 "${HEALTHCHECK_URL}" >"${bundle_dir}/health.json" 2>"${bundle_dir}/health-error.txt" || true

if [[ -d "${LOG_DIR}" ]]; then
  find "${LOG_DIR}" -type f -mmin -"${LOG_MINUTES:-120}" -print0 \
    | tar --null -czf "${bundle_dir}/application-logs.tar.gz" --files-from=- 2>/dev/null || true
fi

tar -C "$(dirname "${bundle_dir}")" -czf "${bundle_dir}.tar.gz" "$(basename "${bundle_dir}")"
sha256sum "${bundle_dir}.tar.gz" >"${bundle_dir}.tar.gz.sha256"
chmod 600 "${bundle_dir}.tar.gz" "${bundle_dir}.tar.gz.sha256"
echo "Diagnostic bundle: ${bundle_dir}.tar.gz"
