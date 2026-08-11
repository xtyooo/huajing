/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { TASK_ACTIONS, TASK_STATUS } from '../constants'
import type { TaskLog } from '../types'

const CACHED_MEDIA_DOWNLOAD_ACTIONS = new Set<string>([
  TASK_ACTIONS.IMAGE_GENERATE,
  TASK_ACTIONS.IMAGE_EDIT,
  TASK_ACTIONS.GENERATE,
  TASK_ACTIONS.TEXT_GENERATE,
  TASK_ACTIONS.FIRST_TAIL_GENERATE,
  TASK_ACTIONS.REFERENCE_GENERATE,
  TASK_ACTIONS.REMIX_GENERATE,
])

// getDownloadableCachedMediaUrl 只为已成功缓存的图片或视频任务返回可下载地址，避免继续依赖会过期的上游链接。
export function getDownloadableCachedMediaUrl(
  log: Pick<TaskLog, 'status' | 'action' | 'media_url'>
): string {
  if (
    log.status !== TASK_STATUS.SUCCESS ||
    !CACHED_MEDIA_DOWNLOAD_ACTIONS.has(log.action)
  ) {
    return ''
  }

  return typeof log.media_url === 'string' && /^https?:\/\//.test(log.media_url)
    ? log.media_url
    : ''
}
