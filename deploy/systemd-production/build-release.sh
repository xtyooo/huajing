#!/usr/bin/env bash
set -Eeuo pipefail

# 发布制品默认仅对当前用户可读，避免本地构建过程意外暴露可执行文件。
umask 077

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
REPOSITORY_ROOT=""
ARTIFACT_ROOT=""
RELEASE_ID=""
ALLOW_DIRTY_HEAD=false
TARGET_OS="linux"
TARGET_ARCH="amd64"
UPSTREAM_REF="refs/remotes/upstream/main"

TEMP_CONTEXT=""
TEMP_OUTPUT=""
BUILDER_CONTAINER_ID=""

# 输出脚本用法，并说明非干净工作树只会构建已提交 HEAD 的安全边界。
usage() {
  cat <<'EOF'
用法:
  build-release.sh [--release-id ID] [--allow-dirty-head]

选项:
  --release-id ID       指定发布编号；默认由 HEAD 提交时间和短 SHA 生成。
  --allow-dirty-head    允许工作树非干净，但仍只从已提交的 HEAD 构建。
  -h, --help            显示帮助。

产物:
  release-artifacts/<ID>/main
  release-artifacts/<ID>/main.gz
  release-artifacts/<ID>/SHA256SUMS
  release-artifacts/<ID>/RELEASE-MANIFEST.txt
EOF
}

# 将错误信息写入标准错误并终止构建，让 CI 和人工发布都能明确识别失败。
die() {
  printf '错误: %s\n' "$*" >&2
  exit 1
}

# 记录发布构建的关键阶段，日志中不包含任何认证信息。
log() {
  printf '[build-release] %s\n' "$*"
}

# 解析有限的命令行参数，拒绝未知选项以避免误构建。
parse_arguments() {
  while (($# > 0)); do
    case "$1" in
      --release-id)
        (($# >= 2)) || die "--release-id 需要一个参数"
        RELEASE_ID="$2"
        shift 2
        ;;
      --allow-dirty-head)
        ALLOW_DIRTY_HEAD=true
        shift
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      --)
        shift
        (($# == 0)) || die "不支持位置参数"
        ;;
      *)
        die "未知参数: $1"
        ;;
    esac
  done
}

# 在成功、失败或中断时回收临时容器和目录，但特意保留 Docker builder 镜像缓存。
cleanup() {
  local exit_code=$?
  trap - EXIT INT TERM

  if [[ -n "$BUILDER_CONTAINER_ID" ]]; then
    docker rm -f "$BUILDER_CONTAINER_ID" >/dev/null 2>&1 || true
    BUILDER_CONTAINER_ID=""
  fi
  if [[ -n "$TEMP_CONTEXT" && -d "$TEMP_CONTEXT" ]]; then
    rm -rf -- "$TEMP_CONTEXT"
    TEMP_CONTEXT=""
  fi
  if [[ -n "$TEMP_OUTPUT" && -d "$TEMP_OUTPUT" ]]; then
    rm -rf -- "$TEMP_OUTPUT"
    TEMP_OUTPUT=""
  fi

  exit "$exit_code"
}

# 计算文件 SHA-256，自动适配 Linux sha256sum 和 macOS shasum。
sha256_file() {
  local file_path="$1"

  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$file_path" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$file_path" | awk '{print $1}'
  else
    die "未找到 sha256sum 或 shasum"
  fi
}

# 校验构建环境和仓库状态，确保默认发布不夹带未提交内容。
validate_repository() {
  local command_name
  local worktree_status

  for command_name in docker git gzip tar mktemp awk; do
    command -v "$command_name" >/dev/null 2>&1 || die "缺少必需命令: $command_name"
  done

  REPOSITORY_ROOT="$(git -C "$SCRIPT_DIR" rev-parse --show-toplevel 2>/dev/null)" || die "脚本必须位于 Git 仓库中"
  ARTIFACT_ROOT="${REPOSITORY_ROOT}/release-artifacts"
  [[ -f "${REPOSITORY_ROOT}/Dockerfile" ]] || die "仓库根目录缺少 Dockerfile"
  if [[ -L "$ARTIFACT_ROOT" || ( -e "$ARTIFACT_ROOT" && ! -d "$ARTIFACT_ROOT" ) ]]; then
    die "release-artifacts 必须是仓库内的普通目录"
  fi
  if ! command -v sha256sum >/dev/null 2>&1 && ! command -v shasum >/dev/null 2>&1; then
    die "未找到 sha256sum 或 shasum"
  fi

  worktree_status="$(git -C "$REPOSITORY_ROOT" status --porcelain --untracked-files=all)"
  if [[ -n "$worktree_status" && "$ALLOW_DIRTY_HEAD" != true ]]; then
    die "工作树不干净；请先提交/清理变更，或显式使用 --allow-dirty-head 仅构建已提交 HEAD"
  fi
  if [[ -n "$worktree_status" ]]; then
    log "警告：工作树非干净；未提交内容不会进入 Docker 构建上下文"
  fi

  git -C "$REPOSITORY_ROOT" rev-parse --verify 'HEAD^{commit}' >/dev/null 2>&1 || die "当前仓库没有可构建的 HEAD 提交"
  git -C "$REPOSITORY_ROOT" rev-parse --verify "${UPSTREAM_REF}^{commit}" >/dev/null 2>&1 || die "缺少 ${UPSTREAM_REF}，无法记录 upstream_commit"
}

# 由 HEAD 提交时间和 SHA 生成稳定且唯一的默认发布编号。
resolve_release_id() {
  local commit_time
  local short_commit

  if [[ -z "$RELEASE_ID" ]]; then
    commit_time="$(TZ=UTC git -C "$REPOSITORY_ROOT" show -s --format='%cd' --date=format-local:'%Y%m%dT%H%M%SZ' HEAD)"
    short_commit="$(git -C "$REPOSITORY_ROOT" rev-parse --short=12 HEAD)"
    RELEASE_ID="${commit_time}-${short_commit}"
  fi

  [[ "$RELEASE_ID" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]] || die "RELEASE_ID 只能包含字母、数字、点、下划线和连字符"
}

# 从已提交 HEAD 生成独立 Docker 上下文，从机制上排除未提交文件干扰可复现构建。
prepare_committed_context() {
  TEMP_CONTEXT="$(mktemp -d "${ARTIFACT_ROOT}/.build-context.XXXXXX")"

  git -C "$REPOSITORY_ROOT" archive --format=tar HEAD | tar -xf - -C "$TEMP_CONTEXT"
  [[ -f "${TEMP_CONTEXT}/Dockerfile" ]] || die "HEAD 提交中缺少 Dockerfile"
}

# 使用项目 Dockerfile 的 builder2 阶段构建 linux/amd64，并从临时容器提取 main。
build_and_extract_main() {
  local iid_file="${TEMP_OUTPUT}/builder.iid"
  local builder_image_id

  log "使用 Dockerfile builder2 构建 ${TARGET_OS}/${TARGET_ARCH}"
  docker build \
    --platform "${TARGET_OS}/${TARGET_ARCH}" \
    --target builder2 \
    --build-arg "TARGETOS=${TARGET_OS}" \
    --build-arg "TARGETARCH=${TARGET_ARCH}" \
    --iidfile "$iid_file" \
    --file "${TEMP_CONTEXT}/Dockerfile" \
    "$TEMP_CONTEXT"

  [[ -s "$iid_file" ]] || die "Docker 未返回 builder2 镜像 ID"
  builder_image_id="$(tr -d '\r\n' < "$iid_file")"
  [[ -n "$builder_image_id" ]] || die "builder2 镜像 ID 为空"

  BUILDER_CONTAINER_ID="$(docker create "$builder_image_id")"
  [[ -n "$BUILDER_CONTAINER_ID" ]] || die "无法从 builder2 镜像创建临时容器"
  docker cp "${BUILDER_CONTAINER_ID}:/build/new-api" "${TEMP_OUTPUT}/main"
  [[ -s "${TEMP_OUTPUT}/main" ]] || die "builder2 未产生 /build/new-api"

  # 提取完成后立即删除容器；若这里失败，EXIT trap 会再做一次尽力清理。
  docker rm -f "$BUILDER_CONTAINER_ID" >/dev/null
  BUILDER_CONTAINER_ID=""
  rm -f -- "$iid_file"
  chmod 0755 "${TEMP_OUTPUT}/main"
}

# 使用 gzip -n 移除时间戳和原文件名，保证相同 main 生成相同压缩制品。
package_artifacts() {
  gzip -n -9 -c "${TEMP_OUTPUT}/main" > "${TEMP_OUTPUT}/main.gz"
  [[ -s "${TEMP_OUTPUT}/main.gz" ]] || die "main.gz 生成失败"
}

# 生成发布双 SHA 和来源清单，使上传、部署与回滚可追溯到确切提交。
write_release_metadata() {
  local source_commit
  local source_branch
  local upstream_commit
  local built_at
  local main_sha256
  local main_gz_sha256

  source_commit="$(git -C "$REPOSITORY_ROOT" rev-parse HEAD)"
  source_branch="$(git -C "$REPOSITORY_ROOT" symbolic-ref --quiet --short HEAD 2>/dev/null || printf 'detached')"
  upstream_commit="$(git -C "$REPOSITORY_ROOT" merge-base "$source_commit" "$UPSTREAM_REF")" || die "HEAD 与 ${UPSTREAM_REF} 没有共同提交"
  built_at="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
  main_sha256="$(sha256_file "${TEMP_OUTPUT}/main")"
  main_gz_sha256="$(sha256_file "${TEMP_OUTPUT}/main.gz")"

  cat > "${TEMP_OUTPUT}/SHA256SUMS" <<EOF
${main_sha256}  main
${main_gz_sha256}  main.gz
EOF

  cat > "${TEMP_OUTPUT}/RELEASE-MANIFEST.txt" <<EOF
release_id=${RELEASE_ID}
release_tag=release/${RELEASE_ID}
source_commit=${source_commit}
source_branch=${source_branch}
upstream_commit=${upstream_commit}
target_os=${TARGET_OS}
target_arch=${TARGET_ARCH}
built_at=${built_at}
main_sha256=${main_sha256}
main_gz_sha256=${main_gz_sha256}
EOF
}

# 串联仓库校验、隔离构建、制品封装和原子落盘，任一失败都不留下半成品目录。
main() {
  local release_directory

  parse_arguments "$@"
  validate_repository
  resolve_release_id

  release_directory="${ARTIFACT_ROOT}/${RELEASE_ID}"
  if [[ -e "$release_directory" || -L "$release_directory" ]]; then
    die "发布目录已存在，拒绝覆盖: $release_directory"
  fi

  mkdir -p "$ARTIFACT_ROOT"
  TEMP_OUTPUT="$(mktemp -d "${ARTIFACT_ROOT}/.${RELEASE_ID}.tmp.XXXXXX")"
  prepare_committed_context
  build_and_extract_main
  package_artifacts
  write_release_metadata

  # 先在同一文件系统完整生成内容，最后一次 mv 暴露可用发布目录。
  mv "$TEMP_OUTPUT" "$release_directory"
  TEMP_OUTPUT=""

  log "构建完成: $release_directory"
  log "请使用 SHA256SUMS 中的双 SHA 作为 deploy-fast.sh 入参"
}

trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

main "$@"
