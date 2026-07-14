#!/usr/bin/env bash
set -Eeuo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
source "${SCRIPT_DIR}/common.sh"
start_log rollback

[[ "${CONFIRM_ROLLBACK:-}" == "${RELEASE_ID}" ]] || {
  echo "Set CONFIRM_ROLLBACK=${RELEASE_ID} to confirm image rollback" >&2
  exit 1
}
[[ -f "${RELEASE_DIR}/manifest.env" ]] || { echo "Backup manifest not found" >&2; exit 1; }

# shellcheck disable=SC1090
source "${RELEASE_DIR}/manifest.env"
if [[ -f "${RELEASE_DIR}/release-state.env" ]]; then
  # shellcheck disable=SC1090
  source "${RELEASE_DIR}/release-state.env"
fi
rollback_image="${OLD_IMAGE_ID_AT_DEPLOY:-${OLD_IMAGE_ID:-}}"
[[ -n "${rollback_image}" ]] || { echo "Old image is missing from manifest" >&2; exit 1; }

if ! docker image inspect "${rollback_image}" >/dev/null 2>&1; then
  [[ -f "${RELEASE_DIR}/images/old-image.tar.gz" ]] || { echo "Old image archive not found" >&2; exit 1; }
  gzip -dc "${RELEASE_DIR}/images/old-image.tar.gz" | docker image load
fi

if [[ "${RESTORE_DATABASE:-false}" == "true" ]]; then
  [[ "${CONFIRM_DATA_RESTORE:-}" == "RESTORE-${RELEASE_ID}" ]] || {
    echo "Database restore requires CONFIRM_DATA_RESTORE=RESTORE-${RELEASE_ID}" >&2
    exit 1
  }
	record_command compose stop "${APP_SERVICE}"
	case "${DB_TYPE}" in
    postgres)
      compose exec -T "${DB_SERVICE}" dropdb -U "${DB_USER}" --if-exists "${DB_NAME}"
      compose exec -T "${DB_SERVICE}" createdb -U "${DB_USER}" "${DB_NAME}"
			compose exec -T "${DB_SERVICE}" pg_restore -U "${DB_USER}" -d "${DB_NAME}" <"${RELEASE_DIR}/database/postgres.dump"
      ;;
    mysql)
			docker compose -f "${COMPOSE_FILE}" exec -T -e MYSQL_PWD="${DB_PASSWORD:-}" "${DB_SERVICE}" \
				mysql -u "${DB_USER}" -e "DROP DATABASE IF EXISTS \`${DB_NAME}\`; CREATE DATABASE \`${DB_NAME}\`;"
			docker compose -f "${COMPOSE_FILE}" exec -T -e MYSQL_PWD="${DB_PASSWORD:-}" "${DB_SERVICE}" \
				mysql -u "${DB_USER}" "${DB_NAME}" <"${RELEASE_DIR}/database/mysql.sql"
      ;;
    sqlite)
      cp -a "${RELEASE_DIR}/database/one-api.db" "${SQLITE_FILE}"
      ;;
  esac
	record_command release_compose "${rollback_image}" up -d --no-deps "${APP_SERVICE}"
else
	record_command release_compose "${rollback_image}" up -d --no-deps --force-recreate "${APP_SERVICE}"
fi

health_check "${HEALTHCHECK_URL}" 30 3

touch "${RELEASE_DIR}/ROLLED_BACK"
echo "Rollback completed. Database was restored: ${RESTORE_DATABASE:-false}"
