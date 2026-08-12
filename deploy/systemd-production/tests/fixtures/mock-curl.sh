#!/usr/bin/env bash

set -Eeuo pipefail

# mock curl 验证令牌只存在于 600 权限的 config，然后按 URL 返回固定 Gitee API 响应。
output_file=''
config_file=''
method='GET'
url=''
previous=''
data_binary=''
form_value=''
disable_config=false
for argument in "$@"; do
  case "${previous}" in
    --output)
      output_file="${argument}"
      previous=''
      continue
      ;;
    --config)
      config_file="${argument}"
      previous=''
      continue
      ;;
    --request)
      method="${argument}"
      previous=''
      continue
      ;;
    --data-binary)
      data_binary="${argument}"
      previous=''
      continue
      ;;
    --form)
      form_value="${argument}"
      previous=''
      continue
      ;;
  esac
  case "${argument}" in
    --disable|-q)
      disable_config=true
      ;;
    --output|--config|--request|--data-binary|--form)
      previous="${argument}"
      ;;
    http://*|https://*)
      url="${argument}"
      ;;
  esac
done

[[ -n "${output_file}" && -n "${config_file}" && -n "${url}" ]] || exit 91
[[ "${disable_config}" == true && "${1:-}" == '--disable' ]] || exit 90
[[ "$*" != *"${MOCK_EXPECTED_TOKEN}"* ]] || exit 92
if stat -f '%Lp' "${config_file}" >/dev/null 2>&1; then
  config_mode="$(stat -f '%Lp' "${config_file}")"
else
  config_mode="$(stat -c '%a' "${config_file}")"
fi
[[ "${config_mode}" == '600' ]] || exit 93
grep -Fqx "header = \"Authorization: Bearer ${MOCK_EXPECTED_TOKEN}\"" "${config_file}" || exit 94
grep -Fqx 'proto = "=https"' "${config_file}" || exit 95
if grep -Eq '^location([[:space:]]|$)' "${config_file}"; then
  exit 96
fi

call_count="$(<"${MOCK_STATE_DIR}/call-count")"
call_count=$((call_count + 1))
printf '%s\n' "${call_count}" > "${MOCK_STATE_DIR}/call-count"
printf '%s\t%s\t%s\n' "${call_count}" "${method}" "${url}" >> "${MOCK_STATE_DIR}/calls.log"

status='500'
body='{"message":"mock route not found"}'
case "${url}" in
  */releases/tags/*)
    if [[ "${MOCK_SCENARIO}" == 'existing_release' ]]; then
      status='200'
      body="{\"id\":8000,\"tag_name\":\"${MOCK_RELEASE_TAG}\"}"
    elif [[ "${MOCK_SCENARIO}" == 'malformed_release_lookup' ]]; then
      status='200'
      body='{}'
    elif [[ "${MOCK_SCENARIO}" == 'missing_release_404' ]]; then
      # 保留对标准 REST 404 空结果语义的兼容。
      status='404'
      body='{"message":"Not Found"}'
    else
      # 真实 Gitee 私有仓库在 Release tag 不存在时返回 HTTP 200 + null。
      status='200'
      body='null'
    fi
    ;;
  */tags\?*)
    status='200'
    if [[ "${MOCK_SCENARIO}" == 'existing_tag' ]]; then
      body="[{\"name\":\"${MOCK_RELEASE_TAG}\"}]"
    else
      body='[]'
    fi
    ;;
  */releases)
    [[ "${method}" == 'POST' && "${data_binary}" == @* ]] || exit 97
    request_file="${data_binary#@}"
    jq -e --arg tag "${MOCK_RELEASE_TAG}" '
      .tag_name == $tag and
      .name != "" and
      .body != "" and
      (.target_commitish | type == "string" and test("^[0-9a-fA-F]{40}$")) and
      .prerelease == false
    ' "${request_file}" >/dev/null || exit 98
    if [[ "${MOCK_SCENARIO}" == 'create_error' ]]; then
      status='422'
      body="{\"message\":\"mock create rejected: ${MOCK_EXPECTED_TOKEN}\"}"
    else
      status='201'
      body="{\"id\":9001,\"tag_name\":\"${MOCK_RELEASE_TAG}\"}"
    fi
    ;;
  */releases/9001/attach_files)
    [[ "${method}" == 'POST' && "${form_value}" == file=@* ]] || exit 99
    upload_count="$(<"${MOCK_STATE_DIR}/upload-count")"
    upload_count=$((upload_count + 1))
    printf '%s\n' "${upload_count}" > "${MOCK_STATE_DIR}/upload-count"
    printf '%s\n' "${form_value#file=@}" >> "${MOCK_STATE_DIR}/uploaded-files.log"
    if [[ "${MOCK_SCENARIO}" == 'upload_error' && ${upload_count} -eq 1 ]]; then
      status='500'
      body='{"message":"mock upload rejected"}'
    else
      status='201'
      body="{\"id\":$((9100 + upload_count))}"
    fi
    ;;
esac

printf '%s' "${body}" > "${output_file}"
printf '%s' "${status}"
