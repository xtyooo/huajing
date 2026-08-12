#!/usr/bin/env bash
set -Eeuo pipefail

# 即使运维人员误用 bash -x 执行，也不允许私有 Gitee 下载令牌进入发布日志。
set +x

umask 077

SERVICE_NAME="${SERVICE_NAME:-new-api}"
SERVICE_UNIT="${SERVICE_NAME%.service}.service"
SYSTEMD_UNIT_ROOT="${SYSTEMD_UNIT_ROOT:-/etc/systemd/system}"
RELEASE_ROOT="${RELEASE_ROOT:-/apps/releases}"
BACKUP_ROOT="${BACKUP_ROOT:-/apps/backups}"
INTERNAL_HEALTH_URL="${INTERNAL_HEALTH_URL:-http://127.0.0.1:3000/api/status}"
EXTERNAL_HEALTH_URL="${EXTERNAL_HEALTH_URL:-https://huajingapi.top/api/status}"
FULL_BACKUP_UNIT="${FULL_BACKUP_UNIT:-new-api-cos-full-backup.service}"
BINLOG_BACKUP_UNIT="${BINLOG_BACKUP_UNIT:-new-api-cos-binlog-backup.service}"
MAX_FULL_BACKUP_AGE_SECONDS="${MAX_FULL_BACKUP_AGE_SECONDS:-90000}"
MAX_BINLOG_BACKUP_AGE_SECONDS="${MAX_BINLOG_BACKUP_AGE_SECONDS:-1200}"
GITEE_DOWNLOAD_CURL_CONFIG="${GITEE_DOWNLOAD_CURL_CONFIG:-/etc/new-api-deploy/gitee-download.curl.conf}"
SKIP_BACKUP_GATE="${SKIP_BACKUP_GATE:-false}"
CONFIRM_SKIP_BACKUP_GATE="${CONFIRM_SKIP_BACKUP_GATE:-}"

RELEASE_ID="${1:?usage: deploy-fast.sh RELEASE_ID BINARY_SHA256 GZIP_SHA256 ARTIFACT_URL}"
EXPECTED_BINARY_SHA256="${2:?usage: deploy-fast.sh RELEASE_ID BINARY_SHA256 GZIP_SHA256 ARTIFACT_URL}"
EXPECTED_GZIP_SHA256="${3:?usage: deploy-fast.sh RELEASE_ID BINARY_SHA256 GZIP_SHA256 ARTIFACT_URL}"
ARTIFACT_URL="${4:?usage: deploy-fast.sh RELEASE_ID BINARY_SHA256 GZIP_SHA256 ARTIFACT_URL}"

if [[ ! "$RELEASE_ID" =~ ^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$ ]]; then
  printf 'invalid release id: %s\n' "$RELEASE_ID" >&2
  exit 1
fi
if [[ "$SKIP_BACKUP_GATE" != "true" && "$SKIP_BACKUP_GATE" != "false" ]]; then
  printf 'SKIP_BACKUP_GATE must be exactly true or false\n' >&2
  exit 1
fi
if [[ "$SKIP_BACKUP_GATE" == "true" && "$CONFIRM_SKIP_BACKUP_GATE" != "$RELEASE_ID" ]]; then
  printf 'skipping backup gate requires CONFIRM_SKIP_BACKUP_GATE=%s\n' "$RELEASE_ID" >&2
  exit 1
fi
if [[ ! "$EXPECTED_BINARY_SHA256" =~ ^[0-9a-fA-F]{64}$ || ! "$EXPECTED_GZIP_SHA256" =~ ^[0-9a-fA-F]{64}$ ]]; then
  printf 'invalid SHA256 value\n' >&2
  exit 1
fi
EXPECTED_BINARY_SHA256_NORMALIZED="$(printf '%s' "$EXPECTED_BINARY_SHA256" | tr '[:upper:]' '[:lower:]')"
EXPECTED_GZIP_SHA256_NORMALIZED="$(printf '%s' "$EXPECTED_GZIP_SHA256" | tr '[:upper:]' '[:lower:]')"

# 仅允许不含用户信息的 HTTPS 地址，避免明文凭据出现在日志或进程列表中。
if [[ "$ARTIFACT_URL" =~ [[:space:]] ]] || \
   [[ ! "$ARTIFACT_URL" =~ ^https://(\[[0-9A-Fa-f:.]+\]|[^/@:?#]+)(:[0-9]+)?(/[^?#]*)?([\?][^#]*)?$ ]]; then
  printf 'artifact URL must be anonymous HTTPS without user credentials, whitespace, or fragments\n' >&2
  exit 1
fi
ARTIFACT_HOST="$(printf '%s' "${BASH_REMATCH[1]}" | tr '[:upper:]' '[:lower:]')"

GITEE_PRIVATE_DOWNLOAD=false
if [[ "$ARTIFACT_HOST" == "gitee.com" || "$ARTIFACT_HOST" == "gitee.com." ]]; then
  # 私有仓库鉴权只对 qq1u/new-api 的数字 Release/附件 ID 下载端点开放，防止凭据被用于其他仓库。
  if [[ ! "$ARTIFACT_URL" =~ ^https://gitee\.com/api/v5/repos/qq1u/new-api/releases/[0-9]+/attach_files/[0-9]+/download$ ]]; then
    printf 'private Gitee artifact URL must target qq1u/new-api release attachment download endpoint\n' >&2
    exit 1
  fi
  GITEE_PRIVATE_DOWNLOAD=true
fi

RELEASE_DIR="$RELEASE_ROOT/$RELEASE_ID"
BACKUP_DIR="$BACKUP_ROOT/$RELEASE_ID"
DROPIN_DIR="${SYSTEMD_UNIT_ROOT}/${SERVICE_NAME}.service.d"
DROPIN_PATH="$DROPIN_DIR/release.conf"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
LOG_DIR="$BACKUP_DIR/run-logs"
LOG_FILE="$LOG_DIR/deploy-fast.log"
STAGING_DIR=""

# 发布和备份目录一旦进入正式阶段就不得复用，避免覆盖上一次的审计现场。
if [[ -e "$BACKUP_DIR" || -L "$BACKUP_DIR" || -e "$RELEASE_DIR" || -L "$RELEASE_DIR" ]]; then
  printf 'release or backup already exists: %s\n' "$RELEASE_ID" >&2
  exit 1
fi

# 私有 Gitee 下载配置必须在创建发布目录前校验，失败时不留下阻塞同一 RELEASE_ID 重试的残留目录。
if [[ "$GITEE_PRIVATE_DOWNLOAD" == true ]]; then
  if [[ ! -e "$GITEE_DOWNLOAD_CURL_CONFIG" || ! -r "$GITEE_DOWNLOAD_CURL_CONFIG" ]]; then
    printf 'private Gitee download curl config is missing or unreadable: %s\n' "$GITEE_DOWNLOAD_CURL_CONFIG" >&2
    exit 1
  fi
  if [[ ! -f "$GITEE_DOWNLOAD_CURL_CONFIG" || -L "$GITEE_DOWNLOAD_CURL_CONFIG" ]]; then
    printf 'private Gitee download curl config must be a regular non-symbolic-link file: %s\n' "$GITEE_DOWNLOAD_CURL_CONFIG" >&2
    exit 1
  fi
  if ! command -v stat >/dev/null; then
    printf 'stat is required to verify private Gitee curl config ownership and permissions\n' >&2
    exit 1
  fi
  if [[ "$(stat -c '%u' "$GITEE_DOWNLOAD_CURL_CONFIG")" != "0" ]]; then
    printf 'private Gitee download curl config must be owned by root: %s\n' "$GITEE_DOWNLOAD_CURL_CONFIG" >&2
    exit 1
  fi
  if [[ "$(stat -c '%a' "$GITEE_DOWNLOAD_CURL_CONFIG")" != "600" ]]; then
    printf 'private Gitee download curl config must have mode 600: %s\n' "$GITEE_DOWNLOAD_CURL_CONFIG" >&2
    exit 1
  fi

  # 配置只能携带一条 Gitee OAuth 请求头，禁止通过 curl 配置改写 URL、输出文件、代理或重定向策略。
  config_line_number=0
  authorization_header_count=0
  while IFS= read -r config_line || [[ -n "$config_line" ]]; do
    config_line_number=$((config_line_number + 1))
    config_line="${config_line#"${config_line%%[![:space:]]*}"}"
    config_line="${config_line%"${config_line##*[![:space:]]}"}"
    if [[ -z "$config_line" || "$config_line" == \#* ]]; then
      continue
    fi
    if [[ "$config_line" =~ ^header[[:space:]]*=[[:space:]]*\"Authorization:[[:space:]]+(Bearer|token)[[:space:]]+[^\"[:space:]]+\"$ ]]; then
      authorization_header_count=$((authorization_header_count + 1))
      continue
    fi
    printf 'private Gitee curl config contains an unsupported directive at line %d\n' "$config_line_number" >&2
    exit 1
  done < "$GITEE_DOWNLOAD_CURL_CONFIG"
  if (( authorization_header_count != 1 )); then
    printf 'private Gitee curl config must contain exactly one Authorization header\n' >&2
    exit 1
  fi
fi

# 为发布过程输出带 ISO 时间的统一日志，方便故障审计和回滚判断。
log() {
  printf '%s %s\n' "$(date -Is)" "$*"
}

# 清理只用于下载和校验的临时目录，让门禁失败后可安全复用同一发布编号。
cleanup_staging() {
  if [[ -z "$STAGING_DIR" || ! -e "$STAGING_DIR" ]]; then
    return
  fi

  case "$STAGING_DIR" in
    "$RELEASE_ROOT"/."$RELEASE_ID".staging.*)
      rm -rf -- "$STAGING_DIR"
      STAGING_DIR=""
      ;;
    *)
      log "refusing to clean unexpected staging directory: $STAGING_DIR"
      ;;
  esac
}
trap cleanup_staging EXIT

# 确认指定备份任务最近一次成功时间在发布可接受窗口内。
check_recent_backup() {
  local unit="$1"
  local maximum_age="$2"
  local result timestamp epoch now age
  result="$(systemctl show "$unit" -p Result --value)"
  timestamp="$(systemctl show "$unit" -p ExecMainExitTimestamp --value)"
  if [[ "$result" != "success" ]]; then
    log "backup did not succeed: unit=$unit result=$result"
    return 1
  fi
  if [[ -z "$timestamp" || "$timestamp" == "n/a" ]]; then
    log "backup success timestamp is unavailable: unit=$unit"
    return 1
  fi
  epoch="$(date -d "$timestamp" +%s)"
  now="$(date +%s)"
  age=$((now - epoch))
  if (( age < 0 || age > maximum_age )); then
    log "backup is stale: unit=$unit age_seconds=$age maximum=$maximum_age"
    return 1
  fi
  log "backup is recent: unit=$unit age_seconds=$age"
}

switch_started=false
service_was_active=false

# 发布失败时恢复原 systemd drop-in，并确保原服务重新运行。
restore_previous_service() {
  local exit_code=$?
  trap - ERR INT TERM
  cleanup_staging
  if [[ "$switch_started" == true ]]; then
    log "fast deployment failed; restoring previous systemd configuration"
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
  log "fast deployment exited with code $exit_code"
  exit "$exit_code"
}
trap restore_previous_service ERR INT TERM

log "fast deployment release=$RELEASE_ID service=$SERVICE_NAME"
for command_name in curl date file gzip journalctl sha256sum systemctl systemd-analyze; do
  command -v "$command_name" >/dev/null
done

systemctl is-active --quiet "$SERVICE_NAME"
service_was_active=true
CURRENT_DIR="$(systemctl show "$SERVICE_NAME" -p WorkingDirectory --value)"
[[ -n "$CURRENT_DIR" && -x "$CURRENT_DIR/main" && -s "$CURRENT_DIR/.env" ]]
curl --disable -fsS --max-time 10 "$INTERNAL_HEALTH_URL" >/dev/null
curl --disable -fsS --max-time 15 "$EXTERNAL_HEALTH_URL" >/dev/null
if [[ "$SKIP_BACKUP_GATE" == "true" ]]; then
  # 只有调用者同时给出开关和当次发布 ID 确认值时才可跳过，并在日志与发布记录中留痕。
  BACKUP_GATE_STATUS="skipped-by-explicit-confirmation"
  log "WARNING: backup freshness gate explicitly skipped for release=$RELEASE_ID"
else
  BACKUP_GATE_STATUS="verified"
  check_recent_backup "$FULL_BACKUP_UNIT" "$MAX_FULL_BACKUP_AGE_SECONDS" || exit 1
  check_recent_backup "$BINLOG_BACKUP_UNIT" "$MAX_BINLOG_BACKUP_AGE_SECONDS" || exit 1
fi

available_kb="$(df -Pk "$RELEASE_ROOT" | awk 'NR==2 {print $4}')"
if (( available_kb < 2 * 1024 * 1024 )); then
  log "less than 2 GB is available under $RELEASE_ROOT"
  exit 1
fi

# 先在 release 根目录的同文件系空间中下载并校验，通过全部门禁后才创建正式发布目录。
STAGING_DIR="$(mktemp -d "${RELEASE_ROOT}/.${RELEASE_ID}.staging.XXXXXX")"

if [[ "$GITEE_PRIVATE_DOWNLOAD" == true ]]; then
  log "downloading private release artifact from Gitee"
  # 首次请求只访问受限 Gitee API，不跟随重定向，避免 Authorization 请求头被发送给附件 CDN。
  gitee_download_metadata="$(
    curl --disable --config "$GITEE_DOWNLOAD_CURL_CONFIG" --proto '=https' --request GET --no-location \
      --output "$STAGING_DIR/main.gz.part" --url "$ARTIFACT_URL" --write-out $'%{http_code}\n%{redirect_url}' \
      --fail --silent --show-error --retry 3 --retry-all-errors --connect-timeout 15 --max-time 900
  )"
  gitee_download_status="${gitee_download_metadata%%$'\n'*}"
  if [[ "$gitee_download_metadata" == *$'\n'* ]]; then
    gitee_redirect_url="${gitee_download_metadata#*$'\n'}"
  else
    gitee_redirect_url=""
  fi

  if [[ "$gitee_download_status" =~ ^2[0-9]{2}$ && -z "$gitee_redirect_url" ]]; then
    : # Gitee 直接返回文件时，保留首次请求已写入的下载内容。
  elif [[ "$gitee_download_status" =~ ^3[0-9]{2}$ && -n "$gitee_redirect_url" ]]; then
    # Gitee 附件 API 通常返回带短时签名的 foruda.gitee.com 地址，仅允许匹配该 HTTPS CDN 路径。
    if [[ "$gitee_redirect_url" =~ [[:space:]] ]] || \
       [[ ! "$gitee_redirect_url" =~ ^https://foruda\.gitee\.com/attach_file/[0-9]+/[^\?#]+([\?][^#]+)?$ ]]; then
      log "private Gitee attachment returned an unsupported redirect target"
      exit 1
    fi
    # 第二步不读取鉴权配置，只用 API 返回的短时 HTTPS 地址下载附件。
    curl --disable --proto '=https' --proto-redir '=https' --request GET \
      --no-location-trusted --output "$STAGING_DIR/main.gz.part" --url "$gitee_redirect_url" \
      --fail --location --silent --show-error --retry 3 --retry-all-errors --connect-timeout 15 --max-time 900
  else
    log "private Gitee attachment response was not a file or supported redirect: status=$gitee_download_status"
    exit 1
  fi
else
  log "downloading anonymous release artifact over HTTPS"
  curl --disable --proto '=https' --proto-redir '=https' --request GET \
    --no-location-trusted --output "$STAGING_DIR/main.gz.part" --url "$ARTIFACT_URL" \
    --fail --location --silent --show-error --retry 3 --retry-all-errors --connect-timeout 15 --max-time 900
fi
printf '%s  %s\n' "$EXPECTED_GZIP_SHA256_NORMALIZED" "$STAGING_DIR/main.gz.part" | sha256sum -c -
gzip -t "$STAGING_DIR/main.gz.part"
gzip -dc "$STAGING_DIR/main.gz.part" > "$STAGING_DIR/main.candidate"
printf '%s  %s\n' "$EXPECTED_BINARY_SHA256_NORMALIZED" "$STAGING_DIR/main.candidate" | sha256sum -c -
if ! file -b "$STAGING_DIR/main.candidate" | grep -Eq '^ELF .* executable'; then
  log "decompressed artifact is not an ELF executable"
  exit 1
fi
chmod 700 "$STAGING_DIR/main.candidate"

# 从这里开始才保留正式发布和备份现场；后续失败由回滚流程处理，不自动覆盖记录。
mkdir -p "$RELEASE_DIR" "$LOG_DIR"
exec > >(tee -a "$LOG_FILE") 2>&1
mv "$STAGING_DIR/main.gz.part" "$RELEASE_DIR/main.gz"
mv "$STAGING_DIR/main.candidate" "$RELEASE_DIR/main"
rmdir "$STAGING_DIR"
STAGING_DIR=""
file "$RELEASE_DIR/main"

mkdir -p "$BACKUP_DIR"/{config,files,logs,diagnostics,run-logs}
install -m 600 "$CURRENT_DIR/.env" "$BACKUP_DIR/config/app.env"
install -m 600 "${SYSTEMD_UNIT_ROOT}/${SERVICE_NAME}.service" "$BACKUP_DIR/config/${SERVICE_NAME}.service"
if [[ -f "$DROPIN_PATH" ]]; then
  install -m 600 "$DROPIN_PATH" "$BACKUP_DIR/config/release.conf"
fi
install -m 700 "$CURRENT_DIR/main" "$BACKUP_DIR/files/main.previous"
journalctl -u "$SERVICE_NAME" --since "2 hours ago" --no-pager > "$BACKUP_DIR/logs/journal-before.log"

install -m 600 "$CURRENT_DIR/.env" "$RELEASE_DIR/.env"
if [[ -d "$CURRENT_DIR/logs" && ! -e "$RELEASE_DIR/logs" ]]; then
  # 解析到最终真实目录，避免连续发布形成 release 之间的符号链。
  CURRENT_LOGS_DIR="$(readlink -f "$CURRENT_DIR/logs")"
  [[ -n "$CURRENT_LOGS_DIR" && -d "$CURRENT_LOGS_DIR" ]]
  ln -s "$CURRENT_LOGS_DIR" "$RELEASE_DIR/logs"
fi
printf '%s  %s\n' "$EXPECTED_BINARY_SHA256_NORMALIZED" main > "$RELEASE_DIR/SHA256SUMS"
(cd "$RELEASE_DIR" && sha256sum --check SHA256SUMS)

rollback_source="$(readlink -f "$SCRIPT_DIR/rollback.sh")"
rollback_destination="$(readlink -m "$RELEASE_DIR/rollback.sh")"
if [[ "$rollback_source" != "$rollback_destination" ]]; then
  install -m 700 "$rollback_source" "$rollback_destination"
fi

mkdir -p "$DROPIN_DIR"
switch_started=true
cat > "$DROPIN_PATH.candidate" <<EOF
[Service]
WorkingDirectory=$RELEASE_DIR
ExecStart=
ExecStart=$RELEASE_DIR/main --port 3000
EOF
install -m 600 "$DROPIN_PATH.candidate" "$DROPIN_PATH"
rm -f "$DROPIN_PATH.candidate"
systemctl daemon-reload
systemd-analyze verify "$SERVICE_UNIT"
log "restarting application once"
systemctl restart "$SERVICE_NAME"

health_ok=false
for _attempt in $(seq 1 45); do
  if systemctl is-active --quiet "$SERVICE_NAME" && \
     curl --disable -fsS --max-time 5 "$INTERNAL_HEALTH_URL" > "$BACKUP_DIR/diagnostics/status-internal.json" 2>/dev/null; then
    health_ok=true
    break
  fi
  sleep 2
done
if [[ "$health_ok" != true ]]; then
  log "internal health check did not pass"
  false
fi

curl --disable -fsS --max-time 15 "$EXTERNAL_HEALTH_URL" > "$BACKUP_DIR/diagnostics/status-external.json"
PID="$(systemctl show "$SERVICE_NAME" -p MainPID --value)"
[[ "$(readlink -f "/proc/$PID/exe")" == "$RELEASE_DIR/main" ]]
[[ "$(sha256sum "/proc/$PID/exe" | awk '{print $1}')" == "$EXPECTED_BINARY_SHA256_NORMALIZED" ]]
systemctl is-active --quiet "$SERVICE_NAME"
systemctl show "$SERVICE_NAME" -p ActiveState -p SubState -p MainPID -p NRestarts -p ExecMainStartTimestamp > "$BACKUP_DIR/diagnostics/systemd-after.txt"
systemctl cat "$SERVICE_NAME" > "$BACKUP_DIR/diagnostics/systemd-unit-after.txt"
STARTED_AT="$(systemctl show "$SERVICE_NAME" -p ActiveEnterTimestamp --value)"
journalctl -u "$SERVICE_NAME" --since "$STARTED_AT" --no-pager > "$BACKUP_DIR/logs/journal-after.log"
if grep -aEiq 'panic|fatal|segmentation fault|cache image response failed' "$BACKUP_DIR/logs/journal-after.log"; then
  log "severe error found in journal"
  false
fi
if command -v nginx >/dev/null; then
  nginx -t
fi

(cd "$BACKUP_DIR" && find config files -type f -print0 | sort -z | xargs -0 sha256sum > SHA256SUMS)
(cd "$BACKUP_DIR" && sha256sum --check SHA256SUMS)
cat > "$BACKUP_DIR/RELEASE_RECORD.txt" <<EOF
release_id=$RELEASE_ID
deployed_at=$(date -Is)
deployment_mode=code-only-release-fast
artifact_sha256=$EXPECTED_BINARY_SHA256_NORMALIZED
artifact_gzip_sha256=$EXPECTED_GZIP_SHA256_NORMALIZED
backup_gate=$BACKUP_GATE_STATUS
previous_working_directory=$CURRENT_DIR
release_directory=$RELEASE_DIR
backup_directory=$BACKUP_DIR
internal_health=passed
external_health=passed
rollback_command=CONFIRM_ROLLBACK=$RELEASE_ID $RELEASE_DIR/rollback.sh $RELEASE_ID
EOF

switch_started=false
trap - ERR INT TERM
log "fast deployment completed"
