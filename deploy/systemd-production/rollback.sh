#!/usr/bin/env bash
set -Eeuo pipefail

umask 077

SERVICE_NAME="${SERVICE_NAME:-new-api}"
BACKUP_ROOT="${BACKUP_ROOT:-/apps/backups}"
RELEASE_ID="${1:?usage: CONFIRM_ROLLBACK=RELEASE_ID rollback.sh RELEASE_ID}"
BACKUP_DIR="${BACKUP_ROOT}/${RELEASE_ID}"
DROPIN_PATH="/etc/systemd/system/${SERVICE_NAME}.service.d/release.conf"
LOG_FILE="$BACKUP_DIR/run-logs/rollback.log"

if [[ "${CONFIRM_ROLLBACK:-}" != "$RELEASE_ID" ]]; then
  printf 'set CONFIRM_ROLLBACK=%s to confirm rollback\n' "$RELEASE_ID" >&2
  exit 1
fi

mkdir -p "$BACKUP_DIR/run-logs"
exec > >(tee -a "$LOG_FILE") 2>&1
printf '%s rolling back release %s\n' "$(date -Is)" "$RELEASE_ID"

systemctl stop "$SERVICE_NAME"
if [[ -f "$BACKUP_DIR/config/release.conf" ]]; then
  install -D -m 600 "$BACKUP_DIR/config/release.conf" "$DROPIN_PATH"
else
  rm -f "$DROPIN_PATH"
fi
systemctl daemon-reload
systemctl start "$SERVICE_NAME"

for attempt in $(seq 1 30); do
  if curl -fsS --max-time 5 http://127.0.0.1:3000/api/status >/dev/null; then
    printf '%s rollback health check passed\n' "$(date -Is)"
    exit 0
  fi
  sleep 2
done

printf '%s rollback health check failed\n' "$(date -Is)" >&2
exit 1

