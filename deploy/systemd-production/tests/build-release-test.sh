#!/usr/bin/env bash
set -Eeuo pipefail

TEST_SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
BUILD_SCRIPT="$(cd "${TEST_SCRIPT_DIR}/.." && pwd -P)/build-release.sh"
MOCK_DOCKER_SOURCE="${TEST_SCRIPT_DIR}/fixtures/docker"
TEST_ROOT=""
FIXTURE_REPOSITORY=""
MOCK_BIN=""
MOCK_DOCKER_LOG=""

# 输出测试失败原因并立即终止，避免后续断言覆盖首个回归信号。
fail() {
  printf '测试失败: %s\n' "$*" >&2
  exit 1
}

# 在测试退出时仅删除 mktemp 创建的隔离仓库，不触碰真实工作树。
cleanup_test_root() {
  if [[ -n "$TEST_ROOT" && -d "$TEST_ROOT" ]]; then
    rm -rf -- "$TEST_ROOT"
  fi
}

# 计算测试文件 SHA-256，与被测脚本一样兼容 macOS 和 Linux。
sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

# 断言指定文件存在，用于保护发布制品的完整性契约。
assert_file_exists() {
  [[ -f "$1" ]] || fail "缺少文件: $1"
}

# 断言文件包含精确文本，用于校验清单字段和 Docker 行为。
assert_file_contains() {
  local file_path="$1"
  local expected_text="$2"
  grep -F -- "$expected_text" "$file_path" >/dev/null || fail "${file_path} 中缺少: ${expected_text}"
}

# 在隔离 Git 仓库中运行构建脚本，并将 Docker 调用导向确定性 mock。
run_build() {
  (
    cd "$FIXTURE_REPOSITORY"
    PATH="${MOCK_BIN}:${PATH}" MOCK_DOCKER_LOG="$MOCK_DOCKER_LOG" \
      "${FIXTURE_REPOSITORY}/deploy/systemd-production/build-release.sh" "$@"
  )
}

# 创建最小可提交测试仓库，并配置 upstream/main 以验证清单追溯。
prepare_fixture_repository() {
  TEST_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/new-api-build-release-test.XXXXXX")"
  FIXTURE_REPOSITORY="${TEST_ROOT}/repository"
  MOCK_BIN="${TEST_ROOT}/mock-bin"
  MOCK_DOCKER_LOG="${TEST_ROOT}/docker.log"

  mkdir -p "${FIXTURE_REPOSITORY}/deploy/systemd-production" "$MOCK_BIN"
  cp "$BUILD_SCRIPT" "${FIXTURE_REPOSITORY}/deploy/systemd-production/build-release.sh"
  cp "$MOCK_DOCKER_SOURCE" "${MOCK_BIN}/docker"
  chmod 0755 "${FIXTURE_REPOSITORY}/deploy/systemd-production/build-release.sh" "${MOCK_BIN}/docker"

  cat > "${FIXTURE_REPOSITORY}/Dockerfile" <<'EOF'
FROM scratch AS builder2
EOF
  cat > "${FIXTURE_REPOSITORY}/.gitignore" <<'EOF'
/release-artifacts/
EOF
  printf 'committed\n' > "${FIXTURE_REPOSITORY}/tracked.txt"
  : > "$MOCK_DOCKER_LOG"

  git -C "$FIXTURE_REPOSITORY" init -q
  git -C "$FIXTURE_REPOSITORY" config user.name 'Build Release Test'
  git -C "$FIXTURE_REPOSITORY" config user.email 'build-release-test@example.invalid'
  git -C "$FIXTURE_REPOSITORY" add .
  GIT_AUTHOR_DATE='2026-08-11T12:34:56Z' GIT_COMMITTER_DATE='2026-08-11T12:34:56Z' \
    git -C "$FIXTURE_REPOSITORY" commit -q -m 'test fixture'
  git -C "$FIXTURE_REPOSITORY" update-ref refs/remotes/upstream/main HEAD
}

# 验证 --help 不依赖 Docker 或仓库状态，便于发布人员先查看用法。
test_help() {
  "$BUILD_SCRIPT" --help | grep -F -- '--allow-dirty-head' >/dev/null || fail "--help 未说明非干净 HEAD 选项"
}

# 验证默认发布编号、双 SHA、完整清单和确定性 gzip 产物。
test_clean_reproducible_build() {
  local expected_release_id
  local release_directory
  local copy_directory
  local source_commit
  local source_branch
  local main_sha256
  local main_gz_sha256

  expected_release_id="20260811T123456Z-$(git -C "$FIXTURE_REPOSITORY" rev-parse --short=12 HEAD)"
  run_build >/dev/null
  release_directory="${FIXTURE_REPOSITORY}/release-artifacts/${expected_release_id}"

  assert_file_exists "${release_directory}/main"
  assert_file_exists "${release_directory}/main.gz"
  assert_file_exists "${release_directory}/SHA256SUMS"
  assert_file_exists "${release_directory}/RELEASE-MANIFEST.txt"
  [[ -x "${release_directory}/main" ]] || fail "main 没有可执行权限"
  [[ "$(wc -l < "${release_directory}/SHA256SUMS" | tr -d ' ')" == "2" ]] || fail "SHA256SUMS 必须恰好包含两行"
  gzip -dc "${release_directory}/main.gz" | cmp -s - "${release_directory}/main" || fail "main.gz 解压后与 main 不一致"

  source_commit="$(git -C "$FIXTURE_REPOSITORY" rev-parse HEAD)"
  source_branch="$(git -C "$FIXTURE_REPOSITORY" symbolic-ref --short HEAD)"
  main_sha256="$(sha256_file "${release_directory}/main")"
  main_gz_sha256="$(sha256_file "${release_directory}/main.gz")"

  assert_file_contains "${release_directory}/SHA256SUMS" "${main_sha256}  main"
  assert_file_contains "${release_directory}/SHA256SUMS" "${main_gz_sha256}  main.gz"
  assert_file_contains "${release_directory}/RELEASE-MANIFEST.txt" "release_id=${expected_release_id}"
  assert_file_contains "${release_directory}/RELEASE-MANIFEST.txt" "release_tag=release/${expected_release_id}"
  assert_file_contains "${release_directory}/RELEASE-MANIFEST.txt" "source_commit=${source_commit}"
  assert_file_contains "${release_directory}/RELEASE-MANIFEST.txt" "source_branch=${source_branch}"
  assert_file_contains "${release_directory}/RELEASE-MANIFEST.txt" "upstream_commit=${source_commit}"
  assert_file_contains "${release_directory}/RELEASE-MANIFEST.txt" "target_os=linux"
  assert_file_contains "${release_directory}/RELEASE-MANIFEST.txt" "target_arch=amd64"
  assert_file_contains "${release_directory}/RELEASE-MANIFEST.txt" "main_sha256=${main_sha256}"
  assert_file_contains "${release_directory}/RELEASE-MANIFEST.txt" "main_gz_sha256=${main_gz_sha256}"
  grep -Eq '^built_at=[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$' \
    "${release_directory}/RELEASE-MANIFEST.txt" || fail "built_at 不是 UTC RFC3339 时间"

  run_build --release-id reproducibility-copy >/dev/null
  copy_directory="${FIXTURE_REPOSITORY}/release-artifacts/reproducibility-copy"
  cmp -s "${release_directory}/main" "${copy_directory}/main" || fail "重复构建的 main 不一致"
  cmp -s "${release_directory}/main.gz" "${copy_directory}/main.gz" || fail "gzip -n 重复构建不确定"
  assert_file_contains "$MOCK_DOCKER_LOG" 'build target=builder2 platform=linux/amd64'
  if grep -F 'rmi' "$MOCK_DOCKER_LOG" >/dev/null; then
    fail "构建脚本不应自动删除 builder 镜像"
  fi
}

# 验证既有发布目录不会被覆盖，保护已上传制品的不可变性。
test_existing_release_is_rejected() {
  if run_build --release-id reproducibility-copy >"${TEST_ROOT}/overwrite.log" 2>&1; then
    fail "脚本覆盖了已有发布目录"
  fi
  assert_file_contains "${TEST_ROOT}/overwrite.log" '拒绝覆盖'
}

# 验证默认拒绝非干净工作树，显式允许后仍只构建已提交 HEAD。
test_dirty_worktree_builds_only_committed_head() {
  printf 'dirty\n' > "${FIXTURE_REPOSITORY}/tracked.txt"
  : > "$MOCK_DOCKER_LOG"

  if run_build --release-id dirty-denied >"${TEST_ROOT}/dirty-denied.log" 2>&1; then
    fail "脚本默认允许了非干净工作树"
  fi
  assert_file_contains "${TEST_ROOT}/dirty-denied.log" '工作树不干净'

  run_build --allow-dirty-head --release-id dirty-head >/dev/null
  assert_file_contains "$MOCK_DOCKER_LOG" 'context_tracked=committed'
  printf 'committed\n' > "${FIXTURE_REPOSITORY}/tracked.txt"
}

# 验证 Docker 提取失败会删除临时容器和半成品，不会暴露伪成功发布。
test_failure_cleans_container_and_partial_output() {
  : > "$MOCK_DOCKER_LOG"
  if MOCK_DOCKER_FAIL_CP=1 run_build --release-id expected-failure >"${TEST_ROOT}/failure.log" 2>&1; then
    fail "mock docker cp 失败时构建仍返回成功"
  fi

  [[ ! -e "${FIXTURE_REPOSITORY}/release-artifacts/expected-failure" ]] || fail "失败构建留下了发布目录"
  assert_file_contains "$MOCK_DOCKER_LOG" 'rm -f mock-builder-container'
  if find "${FIXTURE_REPOSITORY}/release-artifacts" -maxdepth 1 -name '.expected-failure.tmp.*' -print | grep -q .; then
    fail "失败构建留下了临时制品"
  fi
}

# 按从基础契约到失败恢复的顺序执行全部确定性测试。
main() {
  trap cleanup_test_root EXIT
  prepare_fixture_repository
  test_help
  test_clean_reproducible_build
  test_existing_release_is_rejected
  test_dirty_worktree_builds_only_committed_head
  test_failure_cleans_container_and_partial_output
  printf 'build-release tests passed\n'
}

main "$@"
