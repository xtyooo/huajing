#!/usr/bin/env bash

set -Eeuo pipefail

# 此测试用本地 mock curl 覆盖预检、创建、上传与失败路径，不会访问真实 Gitee。
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
PUBLISH_SCRIPT="$(cd "${SCRIPT_DIR}/.." && pwd -P)/publish-gitee-release.sh"
REPOSITORY_ROOT="$(git -C "${SCRIPT_DIR}" rev-parse --show-toplevel)"
TEST_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/new-api-gitee-release-test.XXXXXX")"
MOCK_BIN_DIR="${TEST_ROOT}/bin"
MOCK_STATE_DIR="${TEST_ROOT}/state"
TOKEN_FILE="${TEST_ROOT}/gitee-token"
FAKE_TOKEN='mock_token_for_test_only_123456'
MOCK_CURL_FIXTURE="${SCRIPT_DIR}/fixtures/mock-curl.sh"
MOCK_GIT_FIXTURE="${SCRIPT_DIR}/fixtures/mock-git.sh"
REAL_GIT_BIN="$(command -v git)"

# cleanup 仅清理由 mktemp 创建的测试目录。
cleanup() {
  case "${TEST_ROOT}" in
    "${TMPDIR:-/tmp}"/new-api-gitee-release-test.*)
      rm -rf -- "${TEST_ROOT}"
      ;;
    *)
      printf '测试清理已跳过非预期路径: %s\n' "${TEST_ROOT}" >&2
      ;;
  esac
}
trap cleanup EXIT

# fail 输出测试断言失败原因并终止用例。
fail() {
  printf '测试失败: %s\n' "$*" >&2
  exit 1
}

# assert_contains 确认文本中包含指定的契约片段。
assert_contains() {
  local haystack="$1"
  local needle="$2"
  [[ "${haystack}" == *"${needle}"* ]] || fail "未找到预期文本: ${needle}"
}

# sha256_file 为动态产物生成与发布脚本一致的摘要。
sha256_file() {
  local file_path="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${file_path}" | awk '{print $1}'
    return
  fi
  shasum -a 256 "${file_path}" | awk '{print $1}'
}

# make_artifacts 基于当前 Git HEAD 生成一套可验证的最小发布产物。
make_artifacts() {
  local artifact_dir="$1"
  local release_id="$2"
  local head=''
  local branch=''
  local main_sha=''
  local main_gz_sha=''

  mkdir -p "${artifact_dir}"
  head="$(git -C "${REPOSITORY_ROOT}" rev-parse HEAD)"
  branch="$(git -C "${REPOSITORY_ROOT}" symbolic-ref --short HEAD)"
  printf 'new-api mock release binary: %s\n' "${release_id}" > "${artifact_dir}/main"
  gzip -n -c "${artifact_dir}/main" > "${artifact_dir}/main.gz"
  main_sha="$(sha256_file "${artifact_dir}/main")"
  main_gz_sha="$(sha256_file "${artifact_dir}/main.gz")"

  {
    printf 'release_id=%s\n' "${release_id}"
    printf 'release_tag=release/%s\n' "${release_id}"
    printf 'source_commit=%s\n' "${head}"
    printf 'source_branch=%s\n' "${branch}"
    printf 'upstream_commit=%s\n' "${head}"
    printf 'target_os=linux\n'
    printf 'target_arch=amd64\n'
    printf 'built_at=2026-08-12T00:00:00+08:00\n'
    printf 'main_sha256=%s\n' "${main_sha}"
    printf 'main_gz_sha256=%s\n' "${main_gz_sha}"
  } > "${artifact_dir}/RELEASE-MANIFEST.txt"
  {
    printf '%s  main\n' "${main_sha}"
    printf '%s  main.gz\n' "${main_gz_sha}"
  } > "${artifact_dir}/SHA256SUMS"
}

# reset_mock_state 为每个用例分配独立调用计数，防止用例相互污染。
reset_mock_state() {
  rm -rf -- "${MOCK_STATE_DIR}"
  mkdir -p "${MOCK_STATE_DIR}"
  printf '0\n' > "${MOCK_STATE_DIR}/call-count"
  printf '0\n' > "${MOCK_STATE_DIR}/upload-count"
}

# run_publisher 用 mock curl 和测试令牌执行发布脚本，并保留输出供断言。
run_publisher() {
  local scenario="$1"
  local artifact_dir="$2"
  local stdout_file="$3"
  local stderr_file="$4"

  PATH="${MOCK_BIN_DIR}:${PATH}" \
    MOCK_SCENARIO="${scenario}" \
    MOCK_STATE_DIR="${MOCK_STATE_DIR}" \
    MOCK_EXPECTED_TOKEN="${FAKE_TOKEN}" \
    MOCK_RELEASE_TAG="$(awk -F= '$1 == "release_tag" {print substr($0, index($0, "=") + 1)}' "${artifact_dir}/RELEASE-MANIFEST.txt")" \
    GITEE_TOKEN_FILE="${TOKEN_FILE}" \
    REAL_GIT_BIN="${REAL_GIT_BIN}" \
    MOCK_GIT_CLEAN_STATUS=true \
    bash "${PUBLISH_SCRIPT}" "${artifact_dir}" >"${stdout_file}" 2>"${stderr_file}"
}

# run_publisher_with_xtrace 模拟运维人员误用 bash -x，确认脚本仍然不会把令牌打到输出。
run_publisher_with_xtrace() {
  local artifact_dir="$1"
  local stdout_file="$2"
  local stderr_file="$3"

  PATH="${MOCK_BIN_DIR}:${PATH}" \
    MOCK_SCENARIO='success' \
    MOCK_STATE_DIR="${MOCK_STATE_DIR}" \
    MOCK_EXPECTED_TOKEN="${FAKE_TOKEN}" \
    MOCK_RELEASE_TAG="$(awk -F= '$1 == "release_tag" {print substr($0, index($0, "=") + 1)}' "${artifact_dir}/RELEASE-MANIFEST.txt")" \
    GITEE_TOKEN_FILE="${TOKEN_FILE}" \
    REAL_GIT_BIN="${REAL_GIT_BIN}" \
    MOCK_GIT_CLEAN_STATUS=true \
    bash -x "${PUBLISH_SCRIPT}" "${artifact_dir}" >"${stdout_file}" 2>"${stderr_file}"
}

mkdir -p "${MOCK_BIN_DIR}" "${MOCK_STATE_DIR}"
printf '%s\n' "${FAKE_TOKEN}" > "${TOKEN_FILE}"
chmod 600 "${TOKEN_FILE}"
[[ -x "${MOCK_CURL_FIXTURE}" ]] || fail "mock curl fixture 不存在或不可执行: ${MOCK_CURL_FIXTURE}"
[[ -x "${MOCK_GIT_FIXTURE}" ]] || fail "mock git fixture 不存在或不可执行: ${MOCK_GIT_FIXTURE}"
ln -s "${MOCK_CURL_FIXTURE}" "${MOCK_BIN_DIR}/curl"
ln -s "${MOCK_GIT_FIXTURE}" "${MOCK_BIN_DIR}/git"

# dry-run 必须完成全部本地校验，且不要求令牌或调用 curl。
dry_artifacts="${TEST_ROOT}/artifacts-dry-run"
make_artifacts "${dry_artifacts}" "test-$PPID-$$-dry"
dry_output="$(PATH="${MOCK_BIN_DIR}:${PATH}" REAL_GIT_BIN="${REAL_GIT_BIN}" bash "${PUBLISH_SCRIPT}" --dry-run "${dry_artifacts}")"
assert_contains "${dry_output}" 'dry_run=true'
[[ ! -e "${MOCK_STATE_DIR}/calls.log" ]] || fail '--dry-run 不应调用 curl'

# 双 SHA 中任何一层与实际文件不一致时，必须在联网前阻断发布。
printf 'corrupted\n' >> "${dry_artifacts}/main"
if PATH="${MOCK_BIN_DIR}:${PATH}" REAL_GIT_BIN="${REAL_GIT_BIN}" bash "${PUBLISH_SCRIPT}" --dry-run "${dry_artifacts}" >"${TEST_ROOT}/sha-error.out" 2>"${TEST_ROOT}/sha-error.err"; then
  fail 'main 被篡改后 --dry-run 应失败'
fi
assert_contains "$(cat "${TEST_ROOT}/sha-error.err")" 'main 实际 SHA256 与 manifest 不一致'
[[ ! -e "${MOCK_STATE_DIR}/calls.log" ]] || fail '本地 SHA 校验失败时不应调用 curl'

# manifest 不允许增加未知字段，以保证上传后仍是精确的十字段契约。
manifest_artifacts="${TEST_ROOT}/artifacts-manifest-error"
make_artifacts "${manifest_artifacts}" "test-$PPID-$$-manifest-error"
printf 'unexpected=value\n' >> "${manifest_artifacts}/RELEASE-MANIFEST.txt"
if PATH="${MOCK_BIN_DIR}:${PATH}" REAL_GIT_BIN="${REAL_GIT_BIN}" bash "${PUBLISH_SCRIPT}" --dry-run "${manifest_artifacts}" >"${TEST_ROOT}/manifest-error.out" 2>"${TEST_ROOT}/manifest-error.err"; then
  fail 'manifest 包含未知字段时 --dry-run 应失败'
fi
assert_contains "$(cat "${TEST_ROOT}/manifest-error.err")" 'RELEASE-MANIFEST.txt 必须精确只有 10 行固定字段'
[[ ! -e "${MOCK_STATE_DIR}/calls.log" ]] || fail 'manifest 契约错误时不应调用 curl'

# SHA256SUMS 与 manifest 分别被合法格式的不同摘要篡改时，交叉校验也必须拦截。
sums_artifacts="${TEST_ROOT}/artifacts-sums-error"
make_artifacts "${sums_artifacts}" "test-$PPID-$$-sums-error"
wrong_sha='aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
sed "1s/^[0-9a-fA-F]\{64\}/${wrong_sha}/" "${sums_artifacts}/SHA256SUMS" > "${sums_artifacts}/SHA256SUMS.changed"
mv "${sums_artifacts}/SHA256SUMS.changed" "${sums_artifacts}/SHA256SUMS"
if PATH="${MOCK_BIN_DIR}:${PATH}" REAL_GIT_BIN="${REAL_GIT_BIN}" bash "${PUBLISH_SCRIPT}" --dry-run "${sums_artifacts}" >"${TEST_ROOT}/sums-error.out" 2>"${TEST_ROOT}/sums-error.err"; then
  fail 'SHA256SUMS 与 manifest 不一致时 --dry-run 应失败'
fi
assert_contains "$(cat "${TEST_ROOT}/sums-error.err")" 'SHA256SUMS 中 main 摘要与 manifest 不一致'
[[ ! -e "${MOCK_STATE_DIR}/calls.log" ]] || fail 'SHA256SUMS 交叉校验失败时不应调用 curl'

# 成功路径必须创建一次 Release、上传三个附件，并输出固定下载 API URL。
reset_mock_state
success_artifacts="${TEST_ROOT}/artifacts-success"
make_artifacts "${success_artifacts}" "test-$PPID-$$-success"
run_publisher 'success' "${success_artifacts}" "${TEST_ROOT}/success.out" "${TEST_ROOT}/success.err"
success_output="$(cat "${TEST_ROOT}/success.out")"
assert_contains "${success_output}" 'release_id=9001'
assert_contains "${success_output}" 'attachment_id=9101 file=main.gz'
assert_contains "${success_output}" 'attachment_id=9102 file=RELEASE-MANIFEST.txt'
assert_contains "${success_output}" 'attachment_id=9103 file=SHA256SUMS'
assert_contains "${success_output}" 'download_url=https://gitee.com/api/v5/repos/qq1u/new-api/releases/9001/attach_files/9101/download file=main.gz'
assert_contains "${success_output}" 'deploy_fast_arg_1_release_id=test-'
assert_contains "${success_output}" 'deploy_fast_arg_2_main_sha256='
assert_contains "${success_output}" 'deploy_fast_arg_3_main_gz_sha256='
assert_contains "${success_output}" 'deploy_fast_arg_4_main_gz_download_url=https://gitee.com/api/v5/repos/qq1u/new-api/releases/9001/attach_files/9101/download'
assert_contains "${success_output}" 'main_gz_attachment_id=9101'
[[ "$(cat "${MOCK_STATE_DIR}/call-count")" == '6' ]] || fail '成功路径 API 调用次数不是 6'
[[ "$(basename "$(sed -n '1p' "${MOCK_STATE_DIR}/uploaded-files.log")")" == 'main.gz' ]] || fail '第一个上传附件不是 main.gz'
[[ "$(basename "$(sed -n '2p' "${MOCK_STATE_DIR}/uploaded-files.log")")" == 'RELEASE-MANIFEST.txt' ]] || fail '第二个上传附件不是 RELEASE-MANIFEST.txt'
[[ "$(basename "$(sed -n '3p' "${MOCK_STATE_DIR}/uploaded-files.log")")" == 'SHA256SUMS' ]] || fail '第三个上传附件不是 SHA256SUMS'
! grep -R -Fq "${FAKE_TOKEN}" "${MOCK_STATE_DIR}" || fail '令牌泄漏到 mock 调用日志'

# 调用者使用 bash -x 时，脚本入口也必须立即关闭跟踪以保护令牌。
reset_mock_state
run_publisher_with_xtrace "${success_artifacts}" "${TEST_ROOT}/xtrace.out" "${TEST_ROOT}/xtrace.err"
! grep -Fq "${FAKE_TOKEN}" "${TEST_ROOT}/xtrace.out" || fail 'bash -x 时令牌泄漏到 stdout'
! grep -Fq "${FAKE_TOKEN}" "${TEST_ROOT}/xtrace.err" || fail 'bash -x 时令牌泄漏到 stderr'
! grep -R -Fq "${FAKE_TOKEN}" "${MOCK_STATE_DIR}" || fail 'bash -x 时令牌泄漏到 mock 调用日志'

# 令牌文件权限宽于 600 时必须在网络预检前失败。
reset_mock_state
chmod 640 "${TOKEN_FILE}"
if run_publisher 'success' "${success_artifacts}" "${TEST_ROOT}/token-mode.out" "${TEST_ROOT}/token-mode.err"; then
  fail '令牌文件权限不是 600 时脚本应失败'
fi
assert_contains "$(cat "${TEST_ROOT}/token-mode.err")" 'GITEE_TOKEN_FILE 权限必须严格为 600'
[[ "$(cat "${MOCK_STATE_DIR}/call-count")" == '0' ]] || fail '令牌权限错误时不应调用 curl'
chmod 600 "${TOKEN_FILE}"

# 真实发布必须在读取令牌和联网前拒绝非干净工作树。
reset_mock_state
if PATH="${MOCK_BIN_DIR}:${PATH}" \
  MOCK_SCENARIO='success' \
  MOCK_STATE_DIR="${MOCK_STATE_DIR}" \
  MOCK_EXPECTED_TOKEN="${FAKE_TOKEN}" \
  MOCK_RELEASE_TAG="$(awk -F= '$1 == "release_tag" {print substr($0, index($0, "=") + 1)}' "${success_artifacts}/RELEASE-MANIFEST.txt")" \
  GITEE_TOKEN_FILE="${TOKEN_FILE}" \
  REAL_GIT_BIN="${REAL_GIT_BIN}" \
  MOCK_GIT_CLEAN_STATUS=false \
  bash "${PUBLISH_SCRIPT}" "${success_artifacts}" >"${TEST_ROOT}/dirty.out" 2>"${TEST_ROOT}/dirty.err"; then
  fail '非干净工作树不应允许真实发布'
fi
assert_contains "$(cat "${TEST_ROOT}/dirty.err")" '真实发布要求 Git 工作树完全干净'
[[ "$(cat "${MOCK_STATE_DIR}/call-count")" == '0' ]] || fail '非干净工作树不应调用 curl'

# 创建错误必须返回结构化诊断，且不应继续尝试上传。
reset_mock_state
create_error_artifacts="${TEST_ROOT}/artifacts-create-error"
make_artifacts "${create_error_artifacts}" "test-$PPID-$$-create-error"
if run_publisher 'create_error' "${create_error_artifacts}" "${TEST_ROOT}/create-error.out" "${TEST_ROOT}/create-error.err"; then
  fail '创建 API 返回 422 时脚本应失败'
fi
create_error_text="$(cat "${TEST_ROOT}/create-error.err")"
assert_contains "${create_error_text}" '创建 Gitee Release 失败（HTTP 422）: mock create rejected: ****'
[[ "${create_error_text}" != *"${FAKE_TOKEN}"* ]] || fail 'API 错误响应中的令牌未被脱敏'
[[ "$(cat "${MOCK_STATE_DIR}/call-count")" == '3' ]] || fail '创建失败后不应发起附件请求'

# 上传错误必须提示不完整 Release 的人工清理与新 release_id 要求。
reset_mock_state
upload_error_artifacts="${TEST_ROOT}/artifacts-upload-error"
make_artifacts "${upload_error_artifacts}" "test-$PPID-$$-upload-error"
if run_publisher 'upload_error' "${upload_error_artifacts}" "${TEST_ROOT}/upload-error.out" "${TEST_ROOT}/upload-error.err"; then
  fail '附件 API 返回 500 时脚本应失败'
fi
upload_error_text="$(cat "${TEST_ROOT}/upload-error.err")"
assert_contains "${upload_error_text}" '附件 main.gz 上传失败（HTTP 500）: mock upload rejected'
assert_contains "${upload_error_text}" '本脚本不会复用现有 tag/Release'
[[ "$(cat "${MOCK_STATE_DIR}/call-count")" == '4' ]] || fail '首个附件失败后不应继续上传'

# 远程 tag 已存在时必须在创建 Release 前终止，从根本上禁止复用。
reset_mock_state
existing_tag_artifacts="${TEST_ROOT}/artifacts-existing-tag"
make_artifacts "${existing_tag_artifacts}" "test-$PPID-$$-existing-tag"
if run_publisher 'existing_tag' "${existing_tag_artifacts}" "${TEST_ROOT}/existing-tag.out" "${TEST_ROOT}/existing-tag.err"; then
  fail '远程 tag 已存在时脚本应失败'
fi
existing_tag_text="$(cat "${TEST_ROOT}/existing-tag.err")"
assert_contains "${existing_tag_text}" 'Gitee tag 已存在，不允许复用'
[[ "$(cat "${MOCK_STATE_DIR}/call-count")" == '2' ]] || fail '远程 tag 已存在时不应创建 Release'

# 远程 Release 已存在时要在 tag 分页查询前立即终止，不允许复用。
reset_mock_state
existing_release_artifacts="${TEST_ROOT}/artifacts-existing-release"
make_artifacts "${existing_release_artifacts}" "test-$PPID-$$-existing-release"
if run_publisher 'existing_release' "${existing_release_artifacts}" "${TEST_ROOT}/existing-release.out" "${TEST_ROOT}/existing-release.err"; then
  fail '远程 Release 已存在时脚本应失败'
fi
existing_release_text="$(cat "${TEST_ROOT}/existing-release.err")"
assert_contains "${existing_release_text}" 'Gitee Release 已存在，不允许复用 tag'
[[ "$(cat "${MOCK_STATE_DIR}/call-count")" == '1' ]] || fail '远程 Release 已存在时不应继续预检或创建'

printf 'publish-gitee-release 测试通过\n'
