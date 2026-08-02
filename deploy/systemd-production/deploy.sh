#!/usr/bin/env bash
set -Eeuo pipefail

umask 077

SERVICE_NAME="${SERVICE_NAME:-new-api}"
LEGACY_DIR="${LEGACY_DIR:-/apps/v1.0.0-rc.10}"
RELEASE_ROOT="${RELEASE_ROOT:-/apps/releases}"
BACKUP_ROOT="${BACKUP_ROOT:-/apps/backups}"
INTERNAL_HEALTH_URL="${INTERNAL_HEALTH_URL:-http://127.0.0.1:3000/api/status}"
EXTERNAL_HEALTH_URL="${EXTERNAL_HEALTH_URL:-https://huajingapi.top/api/status}"
NGINX_CONFIG="${NGINX_CONFIG:-/www/server/panel/vhost/nginx/proxy/huajingapi.top/567d5fd54fca66d6aa50ac2f036e1436_huajingapi.top.conf}"

RELEASE_ID="${1:?usage: deploy.sh RELEASE_ID EXPECTED_SHA256}"
EXPECTED_SHA256="${2:?usage: deploy.sh RELEASE_ID EXPECTED_SHA256}"
RELEASE_DIR="${RELEASE_ROOT}/${RELEASE_ID}"
BACKUP_DIR="${BACKUP_ROOT}/${RELEASE_ID}"
UPLOAD_PATH="${RELEASE_DIR}/main.upload"
MAIN_PATH="${RELEASE_DIR}/main"
DROPIN_DIR="/etc/systemd/system/${SERVICE_NAME}.service.d"
DROPIN_PATH="${DROPIN_DIR}/release.conf"
LOG_DIR="${BACKUP_DIR}/run-logs"
LOG_FILE="${LOG_DIR}/deploy.log"

mkdir -p "$RELEASE_DIR" "$LOG_DIR"
exec > >(tee -a "$LOG_FILE") 2>&1

log() {
  printf '%s %s\n' "$(date -Is)" "$*"
}

service_was_active=false
switch_started=false

restore_previous_service() {
  local exit_code=$?
  if [[ "$switch_started" == true ]]; then
    log "deployment failed, restoring the previous systemd configuration"
    if [[ -f "$BACKUP_DIR/config/release.conf" ]]; then
      install -D -m 600 "$BACKUP_DIR/config/release.conf" "$DROPIN_PATH"
    else
      rm -f "$DROPIN_PATH"
    fi
    systemctl daemon-reload || true
    systemctl restart "$SERVICE_NAME" || true
  elif [[ "$service_was_active" == true ]]; then
    systemctl start "$SERVICE_NAME" || true
  fi
  log "deployment exited with code $exit_code"
  exit "$exit_code"
}
trap restore_previous_service ERR INT TERM

log "release=$RELEASE_ID service=$SERVICE_NAME"
for command_name in curl sha256sum systemctl python3 mysqldump mysql zstd rsync; do
  command -v "$command_name" >/dev/null
done
[[ -f "$UPLOAD_PATH" ]]
[[ -f "$LEGACY_DIR/.env" ]]
[[ -f "/etc/systemd/system/${SERVICE_NAME}.service" ]]

actual_sha256="$(sha256sum "$UPLOAD_PATH" | awk '{print $1}')"
if [[ "${actual_sha256,,}" != "${EXPECTED_SHA256,,}" ]]; then
  log "artifact checksum mismatch: expected=$EXPECTED_SHA256 actual=$actual_sha256"
  exit 1
fi

curl -fsS --max-time 10 "$INTERNAL_HEALTH_URL" >/dev/null
curl -fsS --max-time 15 "$EXTERNAL_HEALTH_URL" >/dev/null
available_kb="$(df -Pk "$BACKUP_ROOT" | awk 'NR==2 {print $4}')"
if (( available_kb < 10 * 1024 * 1024 )); then
  log "less than 10 GB is available under $BACKUP_ROOT"
  exit 1
fi

mkdir -p "$BACKUP_DIR"/{config,database,files,logs,diagnostics,run-logs}
install -m 600 "$LEGACY_DIR/.env" "$BACKUP_DIR/config/app.env"
install -m 600 "/etc/systemd/system/${SERVICE_NAME}.service" "$BACKUP_DIR/config/${SERVICE_NAME}.service"
if [[ -f "$DROPIN_PATH" ]]; then
  install -m 600 "$DROPIN_PATH" "$BACKUP_DIR/config/release.conf"
fi
if [[ -f "$NGINX_CONFIG" ]]; then
  install -m 600 "$NGINX_CONFIG" "$BACKUP_DIR/config/nginx-proxy.conf"
fi
install -m 700 "$LEGACY_DIR/main" "$BACKUP_DIR/files/main.previous"
if [[ -f "$LEGACY_DIR/app.log" ]]; then
  tail -n 20000 "$LEGACY_DIR/app.log" | gzip -9 > "$BACKUP_DIR/logs/app-before.log.gz"
else
  : > "$BACKUP_DIR/logs/app-before.log.gz"
fi
journalctl -u "$SERVICE_NAME" --since "2 hours ago" --no-pager > "$BACKUP_DIR/logs/journal-before.log"

systemctl is-active --quiet "$SERVICE_NAME"
service_was_active=true
log "stopping the application for a consistent database and media snapshot"
systemctl stop "$SERVICE_NAME"

if [[ -d "$LEGACY_DIR/media" ]]; then
  cp -al "$LEGACY_DIR/media" "$BACKUP_DIR/files/media"
fi

python3 - "$LEGACY_DIR/.env" "$BACKUP_DIR/database" <<'PY'
import os
import re
import subprocess
import sys
from pathlib import Path

env_path = Path(sys.argv[1])
backup_dir = Path(sys.argv[2])
values = {}
for raw in env_path.read_text(encoding="utf-8").splitlines():
    line = raw.strip()
    if not line or line.startswith("#") or "=" not in line:
        continue
    key, value = line.split("=", 1)
    values[key.strip()] = value.strip().strip('"').strip("'")

dsn = values.get("SQL_DSN", "")
match = re.fullmatch(r"([^:]+):(.*)@tcp\(([^:)]+)(?::([0-9]+))?\)/([^?]+)(?:\?.*)?", dsn)
if not match:
    raise SystemExit("unsupported or missing MySQL SQL_DSN")
user, password, host, port, database = match.groups()
port = port or "3306"
command_env = os.environ.copy()
command_env["MYSQL_PWD"] = password

dump_path = backup_dir / "mysql.sql"
with dump_path.open("wb") as output:
    subprocess.run(
        [
            "mysqldump", "--single-transaction", "--quick", "--routines", "--triggers",
            "--hex-blob", "--set-gtid-purged=OFF", "--host", host, "--port", port,
            "--user", user, "--databases", database,
        ],
        env=command_env,
        stdout=output,
        check=True,
    )

def query(sql):
    result = subprocess.run(
        ["mysql", "--batch", "--skip-column-names", "--host", host, "--port", port,
         "--user", user, database, "--execute", sql],
        env=command_env,
        check=True,
        text=True,
        capture_output=True,
    )
    return result.stdout.strip()

metadata = [
    f"database={database}",
    f"table_count={query('SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE()')}",
]
for name, sql in (
    ("task_count", "SELECT COUNT(*) FROM tasks"),
    ("media_pending", "SELECT COUNT(*) FROM tasks WHERE media_status = 1"),
    ("media_downloading", "SELECT COUNT(*) FROM tasks WHERE media_status = 2"),
    ("media_success", "SELECT COUNT(*) FROM tasks WHERE media_status = 3"),
    ("json_media_success", "SELECT COUNT(*) FROM tasks WHERE media_status = 3 AND LOWER(media_url) LIKE '%.json'"),
):
    try:
        metadata.append(f"{name}={query(sql)}")
    except subprocess.CalledProcessError:
        metadata.append(f"{name}=unavailable")
(backup_dir / "metadata.txt").write_text("\n".join(metadata) + "\n", encoding="utf-8")
PY

test -s "$BACKUP_DIR/database/mysql.sql"
zstd -T0 -19 --rm "$BACKUP_DIR/database/mysql.sql" -o "$BACKUP_DIR/database/mysql.sql.zst"
zstd -t "$BACKUP_DIR/database/mysql.sql.zst"

install -m 700 "$UPLOAD_PATH" "$MAIN_PATH"
install -m 600 "$LEGACY_DIR/.env" "$RELEASE_DIR/.env"
if [[ -d "$LEGACY_DIR/logs" && ! -e "$RELEASE_DIR/logs" ]]; then
  ln -s "$LEGACY_DIR/logs" "$RELEASE_DIR/logs"
fi
printf '%s  %s\n' "$actual_sha256" "main" > "$RELEASE_DIR/SHA256SUMS"
(cd "$RELEASE_DIR" && sha256sum --check SHA256SUMS)

mkdir -p "$DROPIN_DIR"
switch_started=true
cat > "$DROPIN_PATH" <<EOF
[Service]
WorkingDirectory=$RELEASE_DIR
ExecStart=
ExecStart=$MAIN_PATH --port 3000
EOF
chmod 600 "$DROPIN_PATH"
systemctl daemon-reload
systemctl start "$SERVICE_NAME"

for attempt in $(seq 1 30); do
  if curl -fsS --max-time 5 "$INTERNAL_HEALTH_URL" > "$BACKUP_DIR/diagnostics/status-internal.json"; then
    break
  fi
  if (( attempt == 30 )); then
    log "internal health check did not pass"
    false
  fi
  sleep 2
done
curl -fsS --max-time 15 "$EXTERNAL_HEALTH_URL" > "$BACKUP_DIR/diagnostics/status-external.json"
systemctl is-active --quiet "$SERVICE_NAME"
systemctl show "$SERVICE_NAME" -p ActiveState -p SubState -p MainPID -p ExecMainStartTimestamp > "$BACKUP_DIR/diagnostics/systemd-after.txt"
systemctl cat "$SERVICE_NAME" > "$BACKUP_DIR/diagnostics/systemd-unit-after.txt"
if [[ -f "$LEGACY_DIR/app.log" ]]; then
  tail -n 20000 "$LEGACY_DIR/app.log" | gzip -9 > "$BACKUP_DIR/logs/app-after.log.gz"
else
  : > "$BACKUP_DIR/logs/app-after.log.gz"
fi
journalctl -u "$SERVICE_NAME" --since "15 minutes ago" --no-pager > "$BACKUP_DIR/logs/journal-after.log"
df -h / > "$BACKUP_DIR/diagnostics/disk-after.txt"
if [[ -d "$LEGACY_DIR/media" ]]; then
  df -h "$LEGACY_DIR/media" >> "$BACKUP_DIR/diagnostics/disk-after.txt"
fi
free -h > "$BACKUP_DIR/diagnostics/memory-after.txt"

(cd "$BACKUP_DIR" && find config database files -type f -print0 | sort -z | xargs -0 sha256sum > SHA256SUMS)
(cd "$BACKUP_DIR" && sha256sum --check SHA256SUMS)

cat > "$BACKUP_DIR/RELEASE_RECORD.txt" <<EOF
release_id=$RELEASE_ID
deployed_at=$(date -Is)
artifact_sha256=$actual_sha256
previous_working_directory=$LEGACY_DIR
release_directory=$RELEASE_DIR
backup_directory=$BACKUP_DIR
internal_health=passed
external_health=passed
rollback_command=CONFIRM_ROLLBACK=$RELEASE_ID $RELEASE_DIR/rollback.sh $RELEASE_ID
EOF
install -m 700 "$(dirname "$0")/rollback.sh" "$RELEASE_DIR/rollback.sh"

switch_started=false
trap - ERR INT TERM
log "deployment completed and both health checks passed"
