#!/usr/bin/env bash

set -Eeuo pipefail

# 发布脚本必须主动关闭 xtrace，即使调用者使用 bash -x 也不得把令牌展开到日志。
set +x

# 临时凭据和 API 响应默认仅允许当前用户读写。
umask 077

# Gitee 仓库与 API 地址是发布契约的一部分，避免因环境变量误传到其他仓库。
readonly GITEE_API_BASE_URL='https://gitee.com/api/v5'
readonly GITEE_OWNER='qq1u'
readonly GITEE_REPOSITORY='new-api'
readonly DOWNLOAD_API_BASE_URL="${GITEE_API_BASE_URL}/repos/${GITEE_OWNER}/${GITEE_REPOSITORY}"

DRY_RUN=false
TEMP_DIR=''
CURL_CONFIG_FILE=''
CURL_BIN=''
GIT_BIN=''
HTTP_STATUS=''
TOKEN_REDACTION_VALUE=''
LAST_ATTACHMENT_ID=''
LAST_DOWNLOAD_URL=''

# usage 输出最小必要的调用方式和凭据文件要求。
usage() {
  cat >&2 <<'USAGE'
用法:
  publish-gitee-release.sh [--dry-run] <build-release 产物目录>

真实发布时必须设置 GITEE_TOKEN_FILE，且该文件权限必须严格为 600。
--dry-run 只执行本地契约校验，不读取令牌，也不发起任何网络请求。
USAGE
}

# fail 用统一前缀输出可诊断错误，并立即终止发布。
fail() {
  printf '发布失败: %s\n' "$*" >&2
  exit 1
}

# cleanup 只删除本次运行创建的临时目录，防止令牌和 API 响应残留在磁盘。
cleanup() {
  if [[ -n "${TEMP_DIR}" && -d "${TEMP_DIR}" ]]; then
    case "${TEMP_DIR}" in
      "${TMPDIR:-/tmp}"/new-api-gitee-release.*)
        rm -rf -- "${TEMP_DIR}"
        ;;
      *)
        printf '警告: 临时目录路径不符合预期，已跳过清理: %s\n' "${TEMP_DIR}" >&2
        ;;
    esac
  fi
}
trap cleanup EXIT

# require_command 在执行前确认关键工具存在，避免发布到一半才失败。
require_command() {
  local command_name="$1"
  command -v "${command_name}" >/dev/null 2>&1 || fail "缺少必需命令: ${command_name}"
}

# read_manifest_field 严格读取唯一的 key=value 字段，既不 source 文件也不执行其中内容。
read_manifest_field() {
  local key="$1"
  local value=''
  local status=0

  if value="$(awk -v expected_key="${key}" '
    index($0, expected_key "=") == 1 {
      count += 1
      value = substr($0, length(expected_key) + 2)
    }
    END {
      if (count == 0) exit 2
      if (count > 1) exit 3
      print value
    }
  ' "${MANIFEST_FILE}")"; then
    :
  else
    status=$?
    if [[ ${status} -eq 2 ]]; then
      fail "RELEASE-MANIFEST.txt 缺少字段: ${key}"
    fi
    if [[ ${status} -eq 3 ]]; then
      fail "RELEASE-MANIFEST.txt 字段重复: ${key}"
    fi
    fail "无法读取 RELEASE-MANIFEST.txt 字段: ${key}"
  fi

  [[ -n "${value}" ]] || fail "RELEASE-MANIFEST.txt 字段不能为空: ${key}"
  [[ "${value}" != *$'\r'* && "${value}" != *$'\n'* ]] || fail "RELEASE-MANIFEST.txt 字段包含非法换行: ${key}"
  printf '%s' "${value}"
}

# is_sha256 判断字符串是否为完整的 64 位十六进制 SHA256。
is_sha256() {
  local value="$1"
  [[ ${#value} -eq 64 && "${value}" =~ ^[0-9A-Fa-f]+$ ]]
}

# sha256_file 使用当前系统可用的 SHA256 工具计算单个文件摘要。
sha256_file() {
  local file_path="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${file_path}" | awk '{print tolower($1)}'
    return
  fi
  shasum -a 256 "${file_path}" | awk '{print tolower($1)}'
}

# sha256_gzip_payload 直接对 gzip 解压流计算摘要，确认 main.gz 内部确实是同一个 main。
sha256_gzip_payload() {
  local file_path="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    gzip -cd -- "${file_path}" | sha256sum | awk '{print tolower($1)}'
    return
  fi
  gzip -cd -- "${file_path}" | shasum -a 256 | awk '{print tolower($1)}'
}

# file_mode 兼容 macOS 与 GNU stat，用于强制检查令牌文件权限。
file_mode() {
  local file_path="$1"
  if stat -f '%Lp' "${file_path}" >/dev/null 2>&1; then
    stat -f '%Lp' "${file_path}"
    return
  fi
  stat -c '%a' "${file_path}"
}

# json_error_message 仅提取 API 的结构化错误字段，避免把整个响应或敏感信息打到日志。
json_error_message() {
  local response_file="$1"
  local message=''

  message="$(jq -r '(.message // .error // .error_description // empty) | tostring' "${response_file}" 2>/dev/null || true)"
  if [[ -n "${TOKEN_REDACTION_VALUE}" ]]; then
    message="${message//${TOKEN_REDACTION_VALUE}/****}"
  fi
  message="$(printf '%s' "${message}" | tr '\r\n' '  ' | cut -c 1-400)"
  if [[ -z "${message}" ]]; then
    message='响应中没有可诊断的结构化错误字段'
  fi
  printf '%s' "${message}"
}

# api_request 通过临时 curl config 统一携带 Bearer 令牌，令牌不会进入 URL 或命令行参数。
api_request() {
  local method="$1"
  local url="$2"
  local output_file="$3"
  local curl_status=0
  shift 3

  if HTTP_STATUS="$("${CURL_BIN}" \
    --disable \
    --config "${CURL_CONFIG_FILE}" \
    --request "${method}" \
    --output "${output_file}" \
    --write-out '%{http_code}' \
    "$@" \
    "${url}")"; then
    :
  else
    curl_status=$?
    fail "Gitee API 网络请求失败（curl 退出码 ${curl_status}）: ${method} ${url}"
  fi

  [[ "${HTTP_STATUS}" =~ ^[0-9]{3}$ ]] || fail "Gitee API 返回了无效 HTTP 状态码: ${HTTP_STATUS}"
}

# verify_local_contract 校验产物、Git 源头、manifest 以及双 SHA 之间的交叉一致性。
verify_local_contract() {
  local manifest_line_count=''
  local sums_line_count=''
  local main_sum_line=''
  local main_gz_sum_line=''
  local sums_main_sha256=''
  local sums_main_gz_sha256=''
  local actual_main_sha256=''
  local actual_main_gz_sha256=''
  local payload_main_sha256=''
  local current_head=''
  local current_branch=''
  local normalized_source_commit=''
  local normalized_current_head=''

  for required_file in "${MAIN_FILE}" "${MAIN_GZ_FILE}" "${MANIFEST_FILE}" "${SHA256SUMS_FILE}"; do
    [[ -f "${required_file}" && ! -L "${required_file}" ]] || fail "产物文件缺失、不是普通文件或为符号链接: ${required_file}"
  done

  manifest_line_count="$(awk 'END {print NR + 0}' "${MANIFEST_FILE}")"
  [[ ${manifest_line_count} -eq 10 ]] || fail "RELEASE-MANIFEST.txt 必须精确只有 10 行固定字段"
  awk '
    BEGIN {
      expected[1] = "release_id"
      expected[2] = "release_tag"
      expected[3] = "source_commit"
      expected[4] = "source_branch"
      expected[5] = "upstream_commit"
      expected[6] = "target_os"
      expected[7] = "target_arch"
      expected[8] = "built_at"
      expected[9] = "main_sha256"
      expected[10] = "main_gz_sha256"
    }
    {
      separator = index($0, "=")
      key = separator > 0 ? substr($0, 1, separator - 1) : ""
      if (key != expected[NR]) exit 1
    }
  ' "${MANIFEST_FILE}" || fail "RELEASE-MANIFEST.txt 必须按固定顺序包含 10 个契约字段，不允许未知或畸形行"

  RELEASE_ID="$(read_manifest_field release_id)"
  RELEASE_TAG="$(read_manifest_field release_tag)"
  SOURCE_COMMIT="$(read_manifest_field source_commit)"
  SOURCE_BRANCH="$(read_manifest_field source_branch)"
  UPSTREAM_COMMIT="$(read_manifest_field upstream_commit)"
  TARGET_OS="$(read_manifest_field target_os)"
  TARGET_ARCH="$(read_manifest_field target_arch)"
  BUILT_AT="$(read_manifest_field built_at)"
  MAIN_SHA256="$(read_manifest_field main_sha256)"
  MAIN_GZ_SHA256="$(read_manifest_field main_gz_sha256)"

  [[ "${RELEASE_ID}" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]] || fail "release_id 只允许字母、数字、点、下划线和短横线"
  [[ ${#RELEASE_ID} -le 128 ]] || fail "release_id 长度不能超过 128 个字符"
  [[ "${RELEASE_TAG}" == "release/${RELEASE_ID}" ]] || fail "release_tag 必须精确为 release/${RELEASE_ID}"
  "${GIT_BIN}" check-ref-format "refs/tags/${RELEASE_TAG}" >/dev/null 2>&1 || fail "release_tag 不是有效 Git tag: ${RELEASE_TAG}"
  [[ "${SOURCE_COMMIT}" =~ ^[0-9A-Fa-f]{40}$ ]] || fail "source_commit 必须是 40 位完整 Git commit SHA"
  [[ "${UPSTREAM_COMMIT}" =~ ^[0-9A-Fa-f]{40}$ ]] || fail "upstream_commit 必须是 40 位完整 Git commit SHA"
  "${GIT_BIN}" check-ref-format --branch "${SOURCE_BRANCH}" >/dev/null 2>&1 || fail "source_branch 不是有效 Git 分支名: ${SOURCE_BRANCH}"
  [[ "${TARGET_OS}" == 'linux' ]] || fail "target_os 必须为 linux"
  [[ "${TARGET_ARCH}" == 'amd64' ]] || fail "target_arch 必须为 amd64"
  [[ "${BUILT_AT}" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]+)?(Z|[+-][0-9]{2}:[0-9]{2})$ ]] || fail "built_at 必须为带时区的 ISO-8601 时间"
  is_sha256 "${MAIN_SHA256}" || fail "main_sha256 必须是 64 位 SHA256"
  is_sha256 "${MAIN_GZ_SHA256}" || fail "main_gz_sha256 必须是 64 位 SHA256"
  MAIN_SHA256="$(printf '%s' "${MAIN_SHA256}" | tr '[:upper:]' '[:lower:]')"
  MAIN_GZ_SHA256="$(printf '%s' "${MAIN_GZ_SHA256}" | tr '[:upper:]' '[:lower:]')"

  current_head="$("${GIT_BIN}" -C "${REPOSITORY_ROOT}" rev-parse HEAD)"
  normalized_source_commit="$(printf '%s' "${SOURCE_COMMIT}" | tr '[:upper:]' '[:lower:]')"
  normalized_current_head="$(printf '%s' "${current_head}" | tr '[:upper:]' '[:lower:]')"
  [[ "${normalized_source_commit}" == "${normalized_current_head}" ]] || fail "source_commit 与当前 HEAD 不一致: manifest=${SOURCE_COMMIT}, HEAD=${current_head}"
  current_branch="$("${GIT_BIN}" -C "${REPOSITORY_ROOT}" symbolic-ref --quiet --short HEAD)" || fail "当前 Git 工作树处于 detached HEAD，无法验证 source_branch"
  [[ "${SOURCE_BRANCH}" == "${current_branch}" ]] || fail "source_branch 与当前分支不一致: manifest=${SOURCE_BRANCH}, current=${current_branch}"
  "${GIT_BIN}" -C "${REPOSITORY_ROOT}" cat-file -e "${UPSTREAM_COMMIT}^{commit}" 2>/dev/null || fail "upstream_commit 不存在于当前 Git 对象库"
  "${GIT_BIN}" -C "${REPOSITORY_ROOT}" merge-base --is-ancestor "${UPSTREAM_COMMIT}" "${SOURCE_COMMIT}" || fail "upstream_commit 不是 source_commit 的祖先"
  if "${GIT_BIN}" -C "${REPOSITORY_ROOT}" show-ref --verify --quiet "refs/tags/${RELEASE_TAG}"; then
    fail "本地 tag 已存在，不允许复用: ${RELEASE_TAG}"
  fi

  sums_line_count="$(awk 'END {print NR + 0}' "${SHA256SUMS_FILE}")"
  [[ "${sums_line_count}" == '2' ]] || fail "SHA256SUMS 必须精确只有两行"
  main_sum_line="$(sed -n '1p' "${SHA256SUMS_FILE}")"
  main_gz_sum_line="$(sed -n '2p' "${SHA256SUMS_FILE}")"
  sums_main_sha256="${main_sum_line%  main}"
  sums_main_gz_sha256="${main_gz_sum_line%  main.gz}"
  [[ "${main_sum_line}" == "${sums_main_sha256}  main" ]] || fail "SHA256SUMS 第一行必须精确为 HASH  main"
  [[ "${main_gz_sum_line}" == "${sums_main_gz_sha256}  main.gz" ]] || fail "SHA256SUMS 第二行必须精确为 HASH  main.gz"
  is_sha256 "${sums_main_sha256}" || fail "SHA256SUMS 中 main 的摘要不是 64 位 SHA256"
  is_sha256 "${sums_main_gz_sha256}" || fail "SHA256SUMS 中 main.gz 的摘要不是 64 位 SHA256"
  sums_main_sha256="$(printf '%s' "${sums_main_sha256}" | tr '[:upper:]' '[:lower:]')"
  sums_main_gz_sha256="$(printf '%s' "${sums_main_gz_sha256}" | tr '[:upper:]' '[:lower:]')"

  actual_main_sha256="$(sha256_file "${MAIN_FILE}")"
  actual_main_gz_sha256="$(sha256_file "${MAIN_GZ_FILE}")"
  [[ "${actual_main_sha256}" == "${MAIN_SHA256}" ]] || fail "main 实际 SHA256 与 manifest 不一致"
  [[ "${actual_main_gz_sha256}" == "${MAIN_GZ_SHA256}" ]] || fail "main.gz 实际 SHA256 与 manifest 不一致"
  [[ "${sums_main_sha256}" == "${MAIN_SHA256}" ]] || fail "SHA256SUMS 中 main 摘要与 manifest 不一致"
  [[ "${sums_main_gz_sha256}" == "${MAIN_GZ_SHA256}" ]] || fail "SHA256SUMS 中 main.gz 摘要与 manifest 不一致"

  gzip -t -- "${MAIN_GZ_FILE}" || fail "main.gz 完整性校验失败"
  if payload_main_sha256="$(sha256_gzip_payload "${MAIN_GZ_FILE}")"; then
    :
  else
    fail "无法解压 main.gz 并计算内部 main 的 SHA256"
  fi
  [[ "${payload_main_sha256}" == "${MAIN_SHA256}" ]] || fail "main.gz 解压后的内容与 main 不一致"
}

# verify_clean_worktree_for_publish 只在真实发布时要求工作树完全干净，防止 HEAD 诉求与未提交源码状态混用。
verify_clean_worktree_for_publish() {
  local worktree_status=''

  worktree_status="$("${GIT_BIN}" -C "${REPOSITORY_ROOT}" status --porcelain --untracked-files=all)"
  [[ -z "${worktree_status}" ]] || fail "真实发布要求 Git 工作树完全干净；请先提交或清理所有已跟踪和未跟踪变更"
}

# prepare_curl_credentials 安全读取 600 权限的令牌文件，并生成仅当次可读的 curl 配置。
prepare_curl_credentials() {
  local token_file="${GITEE_TOKEN_FILE:-}"
  local token_mode=''
  local token=''
  local token_line_count=''

  [[ -n "${token_file}" ]] || fail "真实发布必须设置 GITEE_TOKEN_FILE"
  [[ -f "${token_file}" && ! -L "${token_file}" ]] || fail "GITEE_TOKEN_FILE 必须指向普通文件，且不能是符号链接"
  token_mode="$(file_mode "${token_file}")"
  [[ "${token_mode}" == '600' ]] || fail "GITEE_TOKEN_FILE 权限必须严格为 600，当前为 ${token_mode}"

  token_line_count="$(awk 'END {print NR + 0}' "${token_file}")"
  [[ "${token_line_count}" == '1' ]] || fail "GITEE_TOKEN_FILE 必须精确只包含一行令牌"
  IFS= read -r token < "${token_file}" || true
  [[ -n "${token}" ]] || fail "GITEE_TOKEN_FILE 中的令牌不能为空"
  [[ "${token}" =~ ^[A-Za-z0-9._~-]+$ ]] || fail "GITEE_TOKEN_FILE 包含非法字符"
  TOKEN_REDACTION_VALUE="${token}"

  TEMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/new-api-gitee-release.XXXXXX")"
  chmod 700 "${TEMP_DIR}"
  CURL_CONFIG_FILE="${TEMP_DIR}/curl.conf"
  {
    printf 'silent\n'
    printf 'show-error\n'
    printf 'proto = "=https"\n'
    printf 'connect-timeout = 15\n'
    printf 'max-time = 1800\n'
    printf 'header = "Accept: application/json"\n'
    printf 'header = "Authorization: Bearer %s"\n' "${token}"
  } > "${CURL_CONFIG_FILE}"
  chmod 600 "${CURL_CONFIG_FILE}"

  # 配置写入后立即清空变量，后续 curl 只通过文件接触令牌。
  token=''
  unset token
}

# ensure_remote_tag_is_unique 同时检查 Release 和 Git tag，确保新发布绝不复用旧标识。
ensure_remote_tag_is_unique() {
  local encoded_tag=''
  local response_file="${TEMP_DIR}/preflight-release.json"
  local tags_file="${TEMP_DIR}/preflight-tags.json"
  local page=1
  local tag_count=0

  encoded_tag="$(jq -nr --arg value "${RELEASE_TAG}" '$value | @uri')"
  api_request 'GET' "${DOWNLOAD_API_BASE_URL}/releases/tags/${encoded_tag}" "${response_file}"
  case "${HTTP_STATUS}" in
    200)
      fail "Gitee Release 已存在，不允许复用 tag: ${RELEASE_TAG}"
      ;;
    404)
      ;;
    *)
      fail "检查 Gitee Release 唯一性失败（HTTP ${HTTP_STATUS}）: $(json_error_message "${response_file}")"
      ;;
  esac

  while :; do
    api_request 'GET' "${DOWNLOAD_API_BASE_URL}/tags?page=${page}&per_page=100" "${tags_file}"
    [[ "${HTTP_STATUS}" == '200' ]] || fail "检查 Gitee tag 唯一性失败（HTTP ${HTTP_STATUS}）: $(json_error_message "${tags_file}")"
    jq -e 'type == "array"' "${tags_file}" >/dev/null 2>&1 || fail "Gitee tag 列表响应不是数组"
    if jq -e --arg tag "${RELEASE_TAG}" 'any(.[]; .name == $tag)' "${tags_file}" >/dev/null; then
      fail "Gitee tag 已存在，不允许复用: ${RELEASE_TAG}"
    fi

    tag_count="$(jq 'length' "${tags_file}")"
    [[ "${tag_count}" =~ ^[0-9]+$ ]] || fail "Gitee tag 列表数量无法解析"
    [[ ${tag_count} -lt 100 ]] && break
    page=$((page + 1))
    [[ ${page} -le 1000 ]] || fail "Gitee tag 分页超过安全上限，无法确认 tag 唯一性"
  done
}

# create_release 用当前 source_commit 创建唯一 tag 和 Release，并校验 API 返回的标识。
create_release() {
  local request_file="${TEMP_DIR}/create-release.json"
  local response_file="${TEMP_DIR}/create-release-response.json"
  local release_body=''
  local returned_tag=''

  release_body="$(printf 'release_id=%s\nsource_commit=%s\nupstream_commit=%s\nmain_sha256=%s\nmain_gz_sha256=%s' \
    "${RELEASE_ID}" "${SOURCE_COMMIT}" "${UPSTREAM_COMMIT}" "${MAIN_SHA256}" "${MAIN_GZ_SHA256}")"
  jq -n \
    --arg tag_name "${RELEASE_TAG}" \
    --arg name "${RELEASE_ID}" \
    --arg body "${release_body}" \
    --arg target_commitish "${SOURCE_COMMIT}" \
    '{tag_name: $tag_name, name: $name, body: $body, prerelease: false, target_commitish: $target_commitish}' \
    > "${request_file}"

  api_request 'POST' "${DOWNLOAD_API_BASE_URL}/releases" "${response_file}" \
    --header 'Content-Type: application/json' \
    --data-binary "@${request_file}"
  [[ "${HTTP_STATUS}" == '201' ]] || fail "创建 Gitee Release 失败（HTTP ${HTTP_STATUS}）: $(json_error_message "${response_file}")"

  API_RELEASE_ID="$(jq -er '.id | tostring | select(test("^[0-9]+$"))' "${response_file}" 2>/dev/null || true)"
  [[ -n "${API_RELEASE_ID}" ]] || fail "Gitee Release 已创建，但响应缺少有效整数 release_id；请手工检查并删除不完整 Release"
  returned_tag="$(jq -r '.tag_name // empty' "${response_file}")"
  [[ "${returned_tag}" == "${RELEASE_TAG}" ]] || fail "Gitee Release 返回的 tag 与请求不一致；请手工检查并删除 release_id=${API_RELEASE_ID}"

  printf 'release_id=%s\n' "${API_RELEASE_ID}"
}

# upload_attachment 上传单个发布附件，并输出可被部署端固定消费的下载 API URL。
upload_attachment() {
  local file_path="$1"
  local file_name=''
  local response_file=''
  local attachment_id=''

  file_name="$(basename "${file_path}")"
  response_file="${TEMP_DIR}/upload-${file_name}.json"
  api_request 'POST' "${DOWNLOAD_API_BASE_URL}/releases/${API_RELEASE_ID}/attach_files" "${response_file}" \
    --form "file=@${file_path}"
  if [[ "${HTTP_STATUS}" != '201' ]]; then
    fail "附件 ${file_name} 上传失败（HTTP ${HTTP_STATUS}）: $(json_error_message "${response_file}")。release_id=${API_RELEASE_ID} 已创建；请删除不完整 Release 并更换 release_id，本脚本不会复用现有 tag/Release"
  fi

  attachment_id="$(jq -er '.id | tostring | select(test("^[0-9]+$"))' "${response_file}" 2>/dev/null || true)"
  [[ -n "${attachment_id}" ]] || fail "附件 ${file_name} 已上传，但响应缺少有效整数 attachment_id；release_id=${API_RELEASE_ID} 需要手工检查"

  LAST_ATTACHMENT_ID="${attachment_id}"
  LAST_DOWNLOAD_URL="${DOWNLOAD_API_BASE_URL}/releases/${API_RELEASE_ID}/attach_files/${attachment_id}/download"

  printf 'attachment_id=%s file=%s\n' "${attachment_id}" "${file_name}"
  printf 'download_url=%s file=%s\n' "${LAST_DOWNLOAD_URL}" "${file_name}"
}

if [[ "${1:-}" == '--dry-run' ]]; then
  DRY_RUN=true
  shift
fi
if [[ $# -ne 1 ]]; then
  usage
  exit 2
fi

require_command git
require_command awk
require_command sed
require_command gzip
if ! command -v sha256sum >/dev/null 2>&1; then
  require_command shasum
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
GIT_BIN="$(command -v git)"
[[ -x "${GIT_BIN}" ]] || fail "Git 命令不可执行: ${GIT_BIN}"
REPOSITORY_ROOT="$("${GIT_BIN}" -C "${SCRIPT_DIR}" rev-parse --show-toplevel 2>/dev/null)" || fail "脚本必须从 new-api Git 工作树内运行"
ARTIFACT_DIR_INPUT="$1"
[[ -d "${ARTIFACT_DIR_INPUT}" ]] || fail "产物目录不存在: ${ARTIFACT_DIR_INPUT}"
ARTIFACT_DIR="$(cd "${ARTIFACT_DIR_INPUT}" && pwd -P)"
MAIN_FILE="${ARTIFACT_DIR}/main"
MAIN_GZ_FILE="${ARTIFACT_DIR}/main.gz"
MANIFEST_FILE="${ARTIFACT_DIR}/RELEASE-MANIFEST.txt"
SHA256SUMS_FILE="${ARTIFACT_DIR}/SHA256SUMS"

verify_local_contract

if [[ "${DRY_RUN}" == true ]]; then
  printf 'dry_run=true\n'
  printf 'release_tag=%s\n' "${RELEASE_TAG}"
  printf 'source_commit=%s\n' "${SOURCE_COMMIT}"
  printf 'main_sha256=%s\n' "${MAIN_SHA256}"
  printf 'main_gz_sha256=%s\n' "${MAIN_GZ_SHA256}"
  printf '本地校验已通过；--dry-run 未读取令牌，未检查远程 tag/Release，也未发起网络请求。\n'
  exit 0
fi

verify_clean_worktree_for_publish
require_command jq
require_command curl
CURL_BIN="$(command -v curl)"
prepare_curl_credentials
ensure_remote_tag_is_unique
create_release

# 附件集合和顺序也是部署契约：先上传可执行包，再上传描述与校验文件。
upload_attachment "${MAIN_GZ_FILE}"
MAIN_GZ_ATTACHMENT_ID="${LAST_ATTACHMENT_ID}"
MAIN_GZ_DOWNLOAD_URL="${LAST_DOWNLOAD_URL}"
upload_attachment "${MANIFEST_FILE}"
upload_attachment "${SHA256SUMS_FILE}"

# 最后单独输出 deploy-fast.sh 的四个位置参数，避免把 manifest release_id 与 Gitee 整数 release_id 混淆。
printf 'deploy_fast_arg_1_release_id=%s\n' "${RELEASE_ID}"
printf 'deploy_fast_arg_2_main_sha256=%s\n' "${MAIN_SHA256}"
printf 'deploy_fast_arg_3_main_gz_sha256=%s\n' "${MAIN_GZ_SHA256}"
printf 'deploy_fast_arg_4_main_gz_download_url=%s\n' "${MAIN_GZ_DOWNLOAD_URL}"
printf 'main_gz_attachment_id=%s\n' "${MAIN_GZ_ATTACHMENT_ID}"
