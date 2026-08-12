#!/usr/bin/env bash

set -Eeuo pipefail

# mock git 仅在发布测试中把 status 视为干净，其余 Git 语义全部交给真实命令。
REAL_GIT_BIN="${REAL_GIT_BIN:?REAL_GIT_BIN 不能为空}"
arguments=("$@")
index=0
while [[ ${index} -lt ${#arguments[@]} ]]; do
  if [[ "${arguments[${index}]}" == 'status' && "${MOCK_GIT_CLEAN_STATUS:-false}" == true ]]; then
    exit 0
  fi
  index=$((index + 1))
done

exec "${REAL_GIT_BIN}" "$@"
