#!/usr/bin/env bash
set -Eeuo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
source "${SCRIPT_DIR}/common.sh"
start_log build-image

require_var SOURCE_DIR
require_var NEW_IMAGE
[[ -f "${SOURCE_DIR}/Dockerfile" ]] || { echo "Dockerfile not found in ${SOURCE_DIR}" >&2; exit 1; }

source_commit="${SOURCE_COMMIT:-}"
if [[ -z "${source_commit}" && -f "${SOURCE_DIR}/RELEASE-METADATA" ]]; then
  source_commit="$(sed -n 's/^commit=//p' "${SOURCE_DIR}/RELEASE-METADATA" | head -n 1)"
fi
source_commit="${source_commit:-uncommitted}"

record_command docker build --pull --platform "${PLATFORM:-linux/amd64}" \
  --label "org.opencontainers.image.version=${RELEASE_ID}" \
  --label "org.opencontainers.image.revision=${source_commit}" \
  --tag "${NEW_IMAGE}" "${SOURCE_DIR}"

docker image inspect "${NEW_IMAGE}" >"${RELEASE_DIR}/new-image-inspect.json"
new_image_id="$(docker image inspect "${NEW_IMAGE}" --format '{{.Id}}')"
write_state_value NEW_IMAGE "${NEW_IMAGE}"
write_state_value NEW_IMAGE_ID "${new_image_id}"

echo "Image ready: ${NEW_IMAGE} (${new_image_id})"
