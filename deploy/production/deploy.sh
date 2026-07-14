#!/usr/bin/env bash
set -Eeuo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
source "${SCRIPT_DIR}/common.sh"
start_log deploy

require_var NEW_IMAGE
[[ -f "${RELEASE_DIR}/BACKUP_VERIFIED" ]] || { echo "Verified backup is required" >&2; exit 1; }
docker image inspect "${NEW_IMAGE}" >/dev/null || { echo "Image not found: ${NEW_IMAGE}" >&2; exit 1; }

old_image_id="$(docker inspect "${APP_CONTAINER}" --format '{{.Image}}')"
write_state_value DEPLOY_START "$(date --iso-8601=seconds)"
write_state_value OLD_IMAGE_ID_AT_DEPLOY "${old_image_id}"

record_command release_compose "${NEW_IMAGE}" config --quiet
record_command release_compose "${NEW_IMAGE}" up -d --no-deps --force-recreate "${APP_SERVICE}"

if ! health_check "${HEALTHCHECK_URL}" 30 3; then
  compose logs --no-color --tail=300 "${APP_SERVICE}" >"${RELEASE_DIR}/failed-deploy-app.log" || true
  echo "Deployment health check failed. Run collect-logs.sh, then rollback.sh." >&2
  exit 1
fi
if [[ -n "${PUBLIC_STATUS_URL:-}" ]]; then
  health_check "${PUBLIC_STATUS_URL}" 10 3
fi

record_command release_compose "${NEW_IMAGE}" ps
record_command docker inspect "${APP_CONTAINER}" --format 'image={{.Image}} started={{.State.StartedAt}} status={{.State.Status}} health={{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}'
write_state_value DEPLOY_FINISH "$(date --iso-8601=seconds)"
touch "${RELEASE_DIR}/DEPLOYED"

echo "Deployment completed. Keep the old image and backup until the observation window ends."
