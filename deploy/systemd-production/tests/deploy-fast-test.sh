#!/usr/bin/env bash
set -Eeuo pipefail

TEST_SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
DEPLOY_SCRIPT="$(cd "${TEST_SCRIPT_DIR}/.." && pwd -P)/deploy-fast.sh"
ROLLBACK_SCRIPT="$(cd "${TEST_SCRIPT_DIR}/.." && pwd -P)/rollback.sh"
REAL_SHA256SUM="$(command -v sha256sum)"
TEST_ROOT=""
MOCK_BIN=""
MOCK_STATE=""
RELEASE_ROOT=""
BACKUP_ROOT=""
SYSTEMD_UNIT_ROOT=""
CURRENT_DIR=""
ARTIFACT_GZIP=""
EXPECTED_BINARY_SHA256=""
EXPECTED_GZIP_SHA256=""

# 输出首个失败断言并终止测试，避免后续错误覆盖根因。
fail() {
  printf '测试失败: %s\n' "$*" >&2
  exit 1
}

# 测试结束时仅删除 mktemp 创建的隔离目录，不触碰仓库或系统发布目录。
cleanup_test_root() {
  if [[ -n "$TEST_ROOT" && -d "$TEST_ROOT" ]]; then
    case "$TEST_ROOT" in
      "${TMPDIR:-/tmp}"/new-api-deploy-fast-test.*) rm -rf -- "$TEST_ROOT" ;;
      *) fail "拒绝清理非预期测试目录: $TEST_ROOT" ;;
    esac
  fi
}

# 断言指定文件包含文本，用于校验拒绝原因和发布记录。
assert_file_contains() {
  local file_path="$1"
  local expected_text="$2"
  grep -F -- "$expected_text" "$file_path" >/dev/null || fail "${file_path} 中缺少: ${expected_text}"
}

# 断言指定文件不包含敏感或越界文本，用于防止鉴权和 URL 泄漏。
assert_file_not_contains() {
  local file_path="$1"
  local unexpected_text="$2"
  if grep -F -- "$unexpected_text" "$file_path" >/dev/null; then
    fail "${file_path} 中不应出现: ${unexpected_text}"
  fi
}

# 执行一个必须失败的前置校验，并断言用户可诊断的错误文本。
expect_validation_failure() {
  local case_name="$1"
  local artifact_url="$2"
  local expected_error="$3"
  local config_path="${4:-${TEST_ROOT}/missing-gitee.curl.conf}"
  local output_path="${TEST_ROOT}/${case_name}.log"

  if PATH="${MOCK_BIN}:/usr/bin:/bin:/usr/sbin:/sbin" \
       RELEASE_ROOT="$RELEASE_ROOT" BACKUP_ROOT="$BACKUP_ROOT" SYSTEMD_UNIT_ROOT="$SYSTEMD_UNIT_ROOT" \
       GITEE_DOWNLOAD_CURL_CONFIG="$config_path" \
       bash "$DEPLOY_SCRIPT" "$case_name" "$EXPECTED_BINARY_SHA256" "$EXPECTED_GZIP_SHA256" "$artifact_url" \
       >"$output_path" 2>&1; then
    fail "${case_name} 未按预期拒绝"
  fi
  assert_file_contains "$output_path" "$expected_error"
  [[ ! -e "${RELEASE_ROOT}/${case_name}" ]] || fail "${case_name} 失败后留下了 release 目录"
  [[ ! -e "${BACKUP_ROOT}/${case_name}" ]] || fail "${case_name} 失败后留下了 backup 目录"
}

# 创建可重复的伪二进制、当前版本和隔离的 systemd/发布目录。
prepare_test_environment() {
  TEST_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/new-api-deploy-fast-test.XXXXXX")"
  MOCK_BIN="${TEST_ROOT}/mock-bin"
  MOCK_STATE="${TEST_ROOT}/mock-state"
  RELEASE_ROOT="${TEST_ROOT}/releases"
  BACKUP_ROOT="${TEST_ROOT}/backups"
  SYSTEMD_UNIT_ROOT="${TEST_ROOT}/systemd"
  CURRENT_DIR="${TEST_ROOT}/current"
  ARTIFACT_GZIP="${TEST_ROOT}/main.gz"

  mkdir -p "$MOCK_BIN" "$MOCK_STATE" "$RELEASE_ROOT" "$BACKUP_ROOT" "$SYSTEMD_UNIT_ROOT" "$CURRENT_DIR/logs"
  printf '#!/bin/sh\nprintf "mock new-api\\n"\n' > "${TEST_ROOT}/artifact-main"
  chmod 700 "${TEST_ROOT}/artifact-main"
  gzip -n -9 -c "${TEST_ROOT}/artifact-main" > "$ARTIFACT_GZIP"
  EXPECTED_BINARY_SHA256="$($REAL_SHA256SUM "${TEST_ROOT}/artifact-main" | awk '{print $1}')"
  EXPECTED_GZIP_SHA256="$($REAL_SHA256SUM "$ARTIFACT_GZIP" | awk '{print $1}')"

  printf '#!/bin/sh\nexit 0\n' > "$CURRENT_DIR/main"
  chmod 700 "$CURRENT_DIR/main"
  printf 'mock=true\n' > "$CURRENT_DIR/.env"
  printf '[Service]\nExecStart=/old/main\n' > "${SYSTEMD_UNIT_ROOT}/new-api.service"

  create_mock_commands
}

# 生成 curl/systemd 等外部依赖的确定性 mock，使测试不访问网络或真实服务。
create_mock_commands() {
  cat > "${MOCK_BIN}/curl" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail

output_path=""
request_url=""
config_path=""
write_out=""
{
  printf 'curl'
  printf ' %q' "$@"
  printf '\n'
} >> "$MOCK_CURL_LOG"

while (( $# > 0 )); do
  case "$1" in
    -o|--output)
      output_path="$2"
      shift 2
      ;;
    --url)
      request_url="$2"
      shift 2
      ;;
    --config)
      config_path="$2"
      shift 2
      ;;
    --write-out)
      write_out="$2"
      shift 2
      ;;
    http://*|https://*)
      request_url="$1"
      shift
      ;;
    *)
      shift
      ;;
  esac
done

if [[ "$request_url" == https://gitee.com/api/v5/repos/qq1u/new-api/releases/* ]]; then
  [[ -n "$config_path" ]] || exit 90
  [[ "$write_out" == *http_code* && "$write_out" == *redirect_url* ]] || exit 91
  if [[ "${MOCK_GITEE_DIRECT:-0}" == "1" ]]; then
    cp "$MOCK_ARTIFACT_GZIP" "$output_path"
    printf '200\n'
    exit 0
  fi
  if [[ -n "$output_path" ]]; then
    printf 'redirect\n' > "$output_path"
  fi
  printf '302\n%s' "${MOCK_GITEE_REDIRECT_URL:-https://foruda.gitee.com/attach_file/1774024389585462037/main.gz?token=mock-signed-url&ts=1786507759&attname=main.gz}"
  exit 0
fi

if [[ "$request_url" == https://foruda.gitee.com/attach_file/* || "$request_url" == https://objects.example.test/* ]]; then
  [[ -z "$config_path" ]] || exit 92
  cp "$MOCK_ARTIFACT_GZIP" "$output_path"
  exit 0
fi

if [[ -n "$output_path" ]]; then
  printf '{"success":true}\n' > "$output_path"
else
  printf '{"success":true}\n'
fi
EOF

  cat > "${MOCK_BIN}/systemctl" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail

printf 'systemctl' >> "$MOCK_SYSTEMCTL_LOG"
printf ' %q' "$@" >> "$MOCK_SYSTEMCTL_LOG"
printf '\n' >> "$MOCK_SYSTEMCTL_LOG"

case "${1:-}" in
  is-active|start|restart|stop|daemon-reload)
    exit 0
    ;;
  cat)
    printf '[Service]\nWorkingDirectory=%s\n' "$MOCK_CURRENT_DIR"
    exit 0
    ;;
  show)
    property=""
    previous=""
    for argument in "$@"; do
      if [[ "$previous" == "-p" ]]; then
        property="$argument"
        break
      fi
      previous="$argument"
    done
    case "$property" in
      Result) printf '%s\n' "${MOCK_BACKUP_RESULT:-success}" ;;
      ExecMainExitTimestamp) printf '2026-08-12 00:00:00 UTC\n' ;;
      WorkingDirectory) printf '%s\n' "$MOCK_CURRENT_DIR" ;;
      MainPID) printf '4242\n' ;;
      ActiveEnterTimestamp) printf '2026-08-12 00:00:00 UTC\n' ;;
      *) printf 'ActiveState=active\nSubState=running\nMainPID=4242\nNRestarts=0\n' ;;
    esac
    exit 0
    ;;
esac

exit 0
EOF

  cat > "${MOCK_BIN}/date" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail

case "${1:-}" in
  -Is) printf '2026-08-12T00:00:00+00:00\n' ;;
  -d) printf '1786492800\n' ;;
  +%s) printf '1786492800\n' ;;
  *) /bin/date "$@" ;;
esac
EOF

  cat > "${MOCK_BIN}/file" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail

if [[ "${MOCK_FILE_NOT_ELF:-0}" == "1" ]]; then
  printf 'ASCII text\n'
elif [[ "${1:-}" == "-b" ]]; then
  printf 'ELF 64-bit LSB pie executable, x86-64\n'
else
  printf '%s: ELF 64-bit LSB pie executable, x86-64\n' "${1:-main}"
fi
EOF

  cat > "${MOCK_BIN}/readlink" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail

if [[ "${1:-}" == "-f" && "${2:-}" == /proc/*/exe ]]; then
  printf '%s\n' "$MOCK_RUNNING_MAIN"
elif [[ "${1:-}" == "-m" ]]; then
  printf '%s\n' "$2"
else
  /usr/bin/readlink "$@"
fi
EOF

  cat > "${MOCK_BIN}/sha256sum" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail

if [[ "${1:-}" == /proc/*/exe ]]; then
  printf '%s  %s\n' "$MOCK_EXPECTED_BINARY_SHA256" "$1"
else
  exec "$MOCK_REAL_SHA256SUM" "$@"
fi
EOF

  cat > "${MOCK_BIN}/journalctl" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
exit 0
EOF

  cat > "${MOCK_BIN}/systemd-analyze" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
exit 0
EOF

  cat > "${MOCK_BIN}/df" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
printf 'Filesystem 1024-blocks Used Available Capacity Mounted on\n'
printf 'mock 104857600 1 104857599 1%% /\n'
EOF

  cat > "${MOCK_BIN}/stat" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail

format="${2:-}"
file_path="${3:-}"
case "$format" in
  %u)
    if [[ "$file_path" == *wrong-owner* ]]; then
      printf '501\n'
    else
      printf '0\n'
    fi
    ;;
  %a)
    if [[ "$file_path" == *wrong-mode* ]]; then
      printf '640\n'
    else
      printf '600\n'
    fi
    ;;
  *) exit 2 ;;
esac
EOF

  chmod 700 "${MOCK_BIN}"/*
}

# 使用隔离目录和 mock 命令执行完整发布流程，不读写真实 /apps 或 systemd。
run_mock_deployment() {
  local release_id="$1"
  local artifact_url="$2"
  local config_path="$3"
  local output_path="$4"
  local expected_gzip_sha256="${5:-$EXPECTED_GZIP_SHA256}"
  local running_main="${RELEASE_ROOT}/${release_id}/main"

  : > "${MOCK_STATE}/curl.log"
  : > "${MOCK_STATE}/systemctl.log"
  PATH="${MOCK_BIN}:/usr/bin:/bin:/usr/sbin:/sbin" \
    RELEASE_ROOT="$RELEASE_ROOT" BACKUP_ROOT="$BACKUP_ROOT" SYSTEMD_UNIT_ROOT="$SYSTEMD_UNIT_ROOT" \
    GITEE_DOWNLOAD_CURL_CONFIG="$config_path" MOCK_CURL_LOG="${MOCK_STATE}/curl.log" \
    MOCK_SYSTEMCTL_LOG="${MOCK_STATE}/systemctl.log" MOCK_CURRENT_DIR="$CURRENT_DIR" \
    MOCK_ARTIFACT_GZIP="$ARTIFACT_GZIP" MOCK_REAL_SHA256SUM="$REAL_SHA256SUM" \
    MOCK_EXPECTED_BINARY_SHA256="$EXPECTED_BINARY_SHA256" MOCK_RUNNING_MAIN="$running_main" \
    MOCK_BACKUP_RESULT="${MOCK_BACKUP_RESULT:-success}" \
    MOCK_GITEE_DIRECT="${MOCK_GITEE_DIRECT:-0}" MOCK_GITEE_REDIRECT_URL="${MOCK_GITEE_REDIRECT_URL:-}" \
    bash "$DEPLOY_SCRIPT" "$release_id" "$EXPECTED_BINARY_SHA256" "$expected_gzip_sha256" "$artifact_url" \
    >"$output_path" 2>&1
}

# 验证 HTTP、凭据 URL、跨仓库 Gitee URL 和不安全 curl 配置都在产生副作用前被拒绝。
test_input_boundaries() {
  local sha="$EXPECTED_BINARY_SHA256"
  local unsafe_config="${TEST_ROOT}/unsafe-gitee.curl.conf"
  local symlink_target="${TEST_ROOT}/symlink-target.curl.conf"
  local wrong_owner_config="${TEST_ROOT}/wrong-owner.curl.conf"
  local wrong_mode_config="${TEST_ROOT}/wrong-mode.curl.conf"

  expect_validation_failure http-url 'http://objects.example.test/main.gz' 'must be anonymous HTTPS'
  expect_validation_failure credential-url 'https://user:password@objects.example.test/main.gz' 'without user credentials'
  expect_validation_failure cross-repo 'https://gitee.com/api/v5/repos/other/new-api/releases/1/attach_files/2/download' 'must target qq1u/new-api'
  expect_validation_failure gitee-port 'https://gitee.com:443/api/v5/repos/qq1u/new-api/releases/1/attach_files/2/download' 'must target qq1u/new-api'
  expect_validation_failure missing-config 'https://gitee.com/api/v5/repos/qq1u/new-api/releases/1/attach_files/2/download' 'curl config is missing or unreadable'

  printf 'header = "Authorization: Bearer mock-token"\nurl = "https://example.test/override"\n' > "$unsafe_config"
  expect_validation_failure unsafe-config 'https://gitee.com/api/v5/repos/qq1u/new-api/releases/1/attach_files/2/download' \
    'unsupported directive at line 2' "$unsafe_config"

  printf 'header = "Authorization: Bearer mock-token"\n' > "$symlink_target"
  ln -s "$symlink_target" "${TEST_ROOT}/symlink-config.curl.conf"
  expect_validation_failure symlink-config 'https://gitee.com/api/v5/repos/qq1u/new-api/releases/1/attach_files/2/download' \
    'must be a regular non-symbolic-link file' "${TEST_ROOT}/symlink-config.curl.conf"

  printf 'header = "Authorization: Bearer mock-token"\n' > "$wrong_owner_config"
  expect_validation_failure wrong-owner 'https://gitee.com/api/v5/repos/qq1u/new-api/releases/1/attach_files/2/download' \
    'must be owned by root' "$wrong_owner_config"

  printf 'header = "Authorization: Bearer mock-token"\n' > "$wrong_mode_config"
  expect_validation_failure wrong-mode 'https://gitee.com/api/v5/repos/qq1u/new-api/releases/1/attach_files/2/download' \
    'must have mode 600' "$wrong_mode_config"

  [[ "$sha" =~ ^[0-9a-f]{64}$ ]] || fail "测试前置 SHA 无效"
}

# 验证 Gitee API 直接返回 2xx 文件时不要求重定向，且仍只使用一次受限鉴权请求。
test_private_gitee_direct_response() {
  local release_id="private-direct-release"
  local config_path="${TEST_ROOT}/gitee-direct.curl.conf"
  local output_path="${TEST_ROOT}/private-direct-release.log"
  local private_url='https://gitee.com/api/v5/repos/qq1u/new-api/releases/9003/attach_files/9301/download'

  printf 'header = "Authorization: Bearer mock-direct-token"\n' > "$config_path"
  MOCK_GITEE_DIRECT=1 run_mock_deployment "$release_id" "$private_url" "$config_path" "$output_path"
  [[ -x "${RELEASE_ROOT}/${release_id}/main" ]] || fail "Gitee 2xx 直返发布未产生 main"
  assert_file_not_contains "${MOCK_STATE}/curl.log" 'https://foruda.gitee.com/'
  [[ "$(grep -c -- '--config' "${MOCK_STATE}/curl.log")" == "1" ]] || fail "Gitee 2xx 直返路径鉴权请求次数错误"
}

# 验证私有 Gitee API 仅在第一跳加载鉴权，CDN 下载不带配置且保留全部发布门禁。
test_private_gitee_release() {
  local release_id="private-release"
  local config_path="${TEST_ROOT}/gitee-download.curl.conf"
  local output_path="${TEST_ROOT}/private-release.log"
  local private_url='https://gitee.com/api/v5/repos/qq1u/new-api/releases/9001/attach_files/9101/download'
  local secret_token='mock-private-token-never-log'

  printf '# Gitee 私有 Release 下载鉴权\nheader = "Authorization: Bearer %s"\n' "$secret_token" > "$config_path"
  chmod 600 "$config_path"
  run_mock_deployment "$release_id" "$private_url" "$config_path" "$output_path"

  [[ -x "${RELEASE_ROOT}/${release_id}/main" ]] || fail "私有 Gitee 发布未产生可执行 main"
  cmp -s "${RELEASE_ROOT}/${release_id}/main" "${TEST_ROOT}/artifact-main" || fail "私有 Gitee main 与双 SHA 校验产物不一致"
  cmp -s "${RELEASE_ROOT}/${release_id}/rollback.sh" "$ROLLBACK_SCRIPT" || fail "发布目录未保留回滚脚本"
  assert_file_contains "${BACKUP_ROOT}/${release_id}/RELEASE_RECORD.txt" 'internal_health=passed'
  assert_file_contains "${BACKUP_ROOT}/${release_id}/RELEASE_RECORD.txt" 'external_health=passed'
  assert_file_contains "${MOCK_STATE}/curl.log" "--config ${config_path}"
  assert_file_contains "${MOCK_STATE}/curl.log" '--disable'
  assert_file_contains "${MOCK_STATE}/curl.log" '--no-location'
  assert_file_contains "${MOCK_STATE}/curl.log" 'https://foruda.gitee.com/attach_file/1774024389585462037/main.gz'
  [[ "$(grep -c -- '--config' "${MOCK_STATE}/curl.log")" == "1" ]] || fail "Gitee curl 配置不是仅用于 API 首跳"
  assert_file_not_contains "${MOCK_STATE}/curl.log" "$secret_token"
  assert_file_not_contains "$output_path" "$secret_token"
  assert_file_contains "${MOCK_STATE}/systemctl.log" 'new-api-cos-full-backup.service'
  assert_file_contains "${MOCK_STATE}/systemctl.log" 'new-api-cos-binlog-backup.service'
  assert_file_contains "${SYSTEMD_UNIT_ROOT}/new-api.service.d/release.conf" "WorkingDirectory=${RELEASE_ROOT}/${release_id}"
}

# 验证任意匿名 HTTPS（包含对象存储签名查询参数）不依赖 Gitee 配置即可发布。
test_anonymous_https_release() {
  local release_id="anonymous-release"
  local output_path="${TEST_ROOT}/anonymous-release.log"
  local anonymous_url='https://objects.example.test/releases/main.gz?signature=temporary'

  run_mock_deployment "$release_id" "$anonymous_url" "${TEST_ROOT}/does-not-exist.conf" "$output_path"
  [[ -x "${RELEASE_ROOT}/${release_id}/main" ]] || fail "匿名 HTTPS 发布未产生 main"
  assert_file_not_contains "${MOCK_STATE}/curl.log" '--config'
  assert_file_contains "${MOCK_STATE}/curl.log" 'https://objects.example.test/releases/main.gz'
}

# 验证备份门禁失败不会占用正式目录，修复备份后可以复用同一发布编号。
test_backup_gate_allows_same_release_retry() {
  local release_id="backup-gate-retry"
  local output_path="${TEST_ROOT}/backup-gate-retry.log"
  local retry_output_path="${TEST_ROOT}/backup-gate-retry-success.log"
  local artifact_url='https://objects.example.test/releases/main.gz'

  if MOCK_BACKUP_RESULT='failed' \
       run_mock_deployment "$release_id" "$artifact_url" "${TEST_ROOT}/does-not-exist.conf" "$output_path"; then
    fail "备份门禁失败时发布仍继续"
  fi
  [[ ! -e "${RELEASE_ROOT}/${release_id}" ]] || fail "备份门禁失败后留下了 release 目录"
  [[ ! -e "${BACKUP_ROOT}/${release_id}" ]] || fail "备份门禁失败后留下了 backup 目录"

  run_mock_deployment "$release_id" "$artifact_url" "${TEST_ROOT}/does-not-exist.conf" "$retry_output_path"
  [[ -x "${RELEASE_ROOT}/${release_id}/main" ]] || fail "备份恢复后同一发布编号未能成功重试"
}

# 验证连续发布始终直接指向最终日志目录，移除中间 release 也不会断链。
test_log_link_resolves_stable_target() {
  local original_current_dir="$CURRENT_DIR"
  local first_release_id="log-link-first"
  local second_release_id="log-link-second"
  local artifact_url='https://objects.example.test/releases/main.gz'
  local second_log_link=''
  local stable_logs_target=''

  run_mock_deployment "$first_release_id" "$artifact_url" "${TEST_ROOT}/does-not-exist.conf" \
    "${TEST_ROOT}/log-link-first.log"
  CURRENT_DIR="${RELEASE_ROOT}/${first_release_id}"
  run_mock_deployment "$second_release_id" "$artifact_url" "${TEST_ROOT}/does-not-exist.conf" \
    "${TEST_ROOT}/log-link-second.log"

  stable_logs_target="$(readlink -f "${original_current_dir}/logs")"
  second_log_link="$(readlink "${RELEASE_ROOT}/${second_release_id}/logs")"
  [[ "$second_log_link" == "$stable_logs_target" ]] || \
    fail "第二次发布的日志链接没有直达稳定目录"
  mv "${RELEASE_ROOT}/${first_release_id}" "${TEST_ROOT}/removed-first-release"
  [[ -d "${RELEASE_ROOT}/${second_release_id}/logs" ]] || fail "移除中间 release 后日志链接断裂"
  CURRENT_DIR="$original_current_dir"
}

# 验证 Gitee API 返回非官方 CDN 目标时不会向该地址发起第二步请求。
test_gitee_redirect_boundary() {
  local release_id="bad-redirect"
  local config_path="${TEST_ROOT}/redirect-gitee.curl.conf"
  local output_path="${TEST_ROOT}/bad-redirect.log"
  local private_url='https://gitee.com/api/v5/repos/qq1u/new-api/releases/9002/attach_files/9201/download'

  printf 'header = "Authorization: token mock-token"\n' > "$config_path"
  if MOCK_GITEE_REDIRECT_URL='https://evil.example.test/other/main.gz' \
       run_mock_deployment "$release_id" "$private_url" "$config_path" "$output_path"; then
    fail "非 Gitee CDN 重定向被接受"
  fi
  assert_file_contains "$output_path" 'unsupported redirect target'
  assert_file_not_contains "${MOCK_STATE}/curl.log" 'https://evil.example.test/other/main.gz'
  [[ ! -e "${RELEASE_ROOT}/${release_id}" ]] || fail "非法跳转失败后留下了 release 目录"
  [[ ! -e "${BACKUP_ROOT}/${release_id}" ]] || fail "非法跳转失败后留下了 backup 目录"
  if find "$RELEASE_ROOT" -maxdepth 1 -name ".${release_id}.staging.*" -print | grep -q .; then
    fail "非法跳转失败后留下了 staging 目录"
  fi

  run_mock_deployment "$release_id" "$private_url" "$config_path" "${TEST_ROOT}/bad-redirect-retry.log"
  [[ -x "${RELEASE_ROOT}/${release_id}/main" ]] || fail "非法跳转失败后同一发布编号未能重试"
}

# 验证 gzip SHA 不匹配时在 systemd 切换前失败，保护双层完整性契约。
test_gzip_sha_boundary() {
  local release_id="bad-gzip-sha"
  local output_path="${TEST_ROOT}/bad-gzip-sha.log"
  local wrong_sha='bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'

  if run_mock_deployment "$release_id" 'https://objects.example.test/releases/main.gz' \
       "${TEST_ROOT}/does-not-exist.conf" "$output_path" "$wrong_sha"; then
    fail "gzip SHA 不匹配时发布仍成功"
  fi
  assert_file_contains "$output_path" 'FAILED'
  assert_file_not_contains "${MOCK_STATE}/systemctl.log" 'restart new-api'
  [[ ! -e "${RELEASE_ROOT}/${release_id}" ]] || fail "gzip SHA 失败后留下了 release 目录"
  [[ ! -e "${BACKUP_ROOT}/${release_id}" ]] || fail "gzip SHA 失败后留下了 backup 目录"
  if find "$RELEASE_ROOT" -maxdepth 1 -name ".${release_id}.staging.*" -print | grep -q .; then
    fail "gzip SHA 失败后留下了 staging 目录"
  fi

  run_mock_deployment "$release_id" 'https://objects.example.test/releases/main.gz' \
    "${TEST_ROOT}/does-not-exist.conf" "${TEST_ROOT}/bad-gzip-sha-retry.log"
  [[ -x "${RELEASE_ROOT}/${release_id}/main" ]] || fail "gzip SHA 失败后同一发布编号未能重试"
}

# 按输入、下载、备份、日志、跳转和完整性边界顺序执行全部测试。
main() {
  trap cleanup_test_root EXIT
  prepare_test_environment
  test_input_boundaries
  test_private_gitee_release
  test_private_gitee_direct_response
  test_anonymous_https_release
  test_backup_gate_allows_same_release_retry
  test_log_link_resolves_stable_target
  test_gitee_redirect_boundary
  test_gzip_sha_boundary
  printf 'deploy-fast tests passed\n'
}

main "$@"
