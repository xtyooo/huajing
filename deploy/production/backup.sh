#!/usr/bin/env bash
set -Eeuo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
source "${SCRIPT_DIR}/common.sh"
start_log backup

if [[ -f "${RELEASE_DIR}/BACKUP_VERIFIED" ]]; then
  echo "A verified backup already exists for ${RELEASE_ID}. Use a new RELEASE_ID instead of overwriting it." >&2
  exit 1
fi

mkdir -p "${RELEASE_DIR}/config" "${RELEASE_DIR}/database" "${RELEASE_DIR}/files" "${RELEASE_DIR}/images"
: >"${RELEASE_DIR}/manifest.env"

app_stopped=false
restart_app() {
  if [[ "${app_stopped}" == "true" ]]; then
    echo "Starting original application after backup operation"
    compose start "${APP_SERVICE}" || true
    app_stopped=false
  fi
}
trap restart_app EXIT

old_image_id="$(docker inspect "${APP_CONTAINER}" --format '{{.Image}}')"
old_image_ref="$(docker inspect "${APP_CONTAINER}" --format '{{.Config.Image}}')"
write_manifest_value RELEASE_ID "${RELEASE_ID}"
write_manifest_value BACKUP_TIME "$(date --iso-8601=seconds)"
write_manifest_value OLD_IMAGE_ID "${old_image_id}"
write_manifest_value OLD_IMAGE_REF "${old_image_ref}"
write_manifest_value COMPOSE_FILE "${COMPOSE_FILE}"
write_manifest_value DB_TYPE "${DB_TYPE}"

cp -a "${COMPOSE_FILE}" "${RELEASE_DIR}/config/docker-compose.yml"
if [[ -f "${APP_DIR}/.env" ]]; then
  cp -a "${APP_DIR}/.env" "${RELEASE_DIR}/config/app.env"
  chmod 600 "${RELEASE_DIR}/config/app.env"
fi
docker inspect "${APP_CONTAINER}" >"${RELEASE_DIR}/config/container-inspect.json"
docker image inspect "${old_image_id}" >"${RELEASE_DIR}/config/image-inspect.json"
compose config >"${RELEASE_DIR}/config/compose-resolved.yml"

for path in ${EXTRA_BACKUP_PATHS:-}; do
  if [[ -e "${path}" ]]; then
    tar -C "$(dirname "${path}")" -czf "${RELEASE_DIR}/config/$(basename "${path}").tar.gz" "$(basename "${path}")"
  fi
done

if [[ "${STOP_APP_FOR_BACKUP:-true}" == "true" ]]; then
  record_command compose stop "${APP_SERVICE}"
  app_stopped=true
fi

case "${DB_TYPE}" in
  postgres)
    compose exec -T "${DB_SERVICE}" pg_dump -U "${DB_USER}" -d "${DB_NAME}" -Fc >"${RELEASE_DIR}/database/postgres.dump"
    compose exec -T "${DB_SERVICE}" pg_restore --list <"${RELEASE_DIR}/database/postgres.dump" >"${RELEASE_DIR}/database/postgres.list"
    ;;
  mysql)
    docker compose -f "${COMPOSE_FILE}" exec -T -e MYSQL_PWD="${DB_PASSWORD:-}" "${DB_SERVICE}" \
      mysqldump -u "${DB_USER}" --single-transaction --routines --triggers "${DB_NAME}" >"${RELEASE_DIR}/database/mysql.sql"
    test -s "${RELEASE_DIR}/database/mysql.sql"
    ;;
  sqlite)
    [[ -f "${SQLITE_FILE}" ]] || { echo "SQLite file not found: ${SQLITE_FILE}" >&2; exit 1; }
    cp -a "${SQLITE_FILE}" "${RELEASE_DIR}/database/one-api.db"
    test -s "${RELEASE_DIR}/database/one-api.db"
    ;;
  *)
    echo "Unsupported DB_TYPE: ${DB_TYPE}" >&2
    exit 1
    ;;
esac

for entry in "media:${MEDIA_DIR}" "logs:${LOG_DIR}" "data:${DATA_DIR}"; do
  name="${entry%%:*}"
  path="${entry#*:}"
  if [[ -n "${path}" && -d "${path}" ]]; then
    tar -C "$(dirname "${path}")" -czf "${RELEASE_DIR}/files/${name}.tar.gz" "$(basename "${path}")"
    tar -tzf "${RELEASE_DIR}/files/${name}.tar.gz" >/dev/null
  fi
done

if [[ "${SAVE_OLD_IMAGE:-true}" == "true" ]]; then
  docker image save "${old_image_id}" | gzip >"${RELEASE_DIR}/images/old-image.tar.gz"
  gzip -t "${RELEASE_DIR}/images/old-image.tar.gz"
fi

restart_app
if [[ "${STOP_APP_FOR_BACKUP:-true}" == "true" ]]; then
  health_check "${HEALTHCHECK_URL}" 30 3
fi

(
  cd "${RELEASE_DIR}"
  find . -type f ! -path "./run-logs/*" ! -name SHA256SUMS -print0 \
    | sort -z \
    | xargs -0 sha256sum >SHA256SUMS
  sha256sum --check SHA256SUMS
)
touch "${RELEASE_DIR}/BACKUP_VERIFIED"
trap - EXIT

echo "Backup verified: ${RELEASE_DIR}"
echo "Do not deploy until this directory has been copied to another storage location."
