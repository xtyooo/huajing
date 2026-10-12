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
import assert from 'node:assert/strict'

import { describe, test } from 'vitest'

import { TASK_ACTIONS, TASK_STATUS } from '../../constants'
import { getDownloadableCachedMediaUrl } from '../task-media'

describe('任务缓存媒体下载', () => {
  test('图片和视频成功缓存后都返回统一下载地址', () => {
    const mediaUrl = 'https://example.test/cached-media'

    assert.equal(
      getDownloadableCachedMediaUrl({
        platform: '81',
        media_status: 3,
        status: TASK_STATUS.SUCCESS,
        action: TASK_ACTIONS.IMAGE_GENERATE,
        media_url: mediaUrl,
      }),
      mediaUrl
    )
    assert.equal(
      getDownloadableCachedMediaUrl({
        platform: '81',
        media_status: 3,
        status: TASK_STATUS.SUCCESS,
        action: TASK_ACTIONS.TEXT_GENERATE,
        media_url: mediaUrl,
      }),
      mediaUrl
    )
  })

  test('失败任务和无效缓存地址不显示下载入口', () => {
    assert.equal(
      getDownloadableCachedMediaUrl({
        platform: '81',
        media_status: 3,
        status: TASK_STATUS.FAILURE,
        action: TASK_ACTIONS.TEXT_GENERATE,
        media_url: 'https://example.test/cached-media',
      }),
      ''
    )
    assert.equal(
      getDownloadableCachedMediaUrl({
        platform: '81',
        media_status: 3,
        status: TASK_STATUS.SUCCESS,
        action: TASK_ACTIONS.TEXT_GENERATE,
        media_url: 'javascript:alert(1)',
      }),
      ''
    )
  })
})

for (const mediaStatus of [-1, 0, 1, 2, 4]) {
  test(`cache status ${mediaStatus} suppresses stale URL even when task is successful`, () => {
    assert.equal(
      getDownloadableCachedMediaUrl({
        platform: '81',
        media_status: mediaStatus,
        status: TASK_STATUS.SUCCESS,
        action: TASK_ACTIONS.GENERATE,
        media_url: 'https://example.test/stale',
      }),
      ''
    )
  })
}
test('completed kuai direct URL remains available without cache and disappears after cleanup', () => {
  const task = {
    platform: '65',
    media_status: 0,
    status: TASK_STATUS.SUCCESS,
    action: TASK_ACTIONS.GENERATE,
    media_url: 'https://example.test/direct',
  }
  assert.equal(getDownloadableCachedMediaUrl(task), task.media_url)
  assert.equal(getDownloadableCachedMediaUrl({ ...task, media_status: 4 }), '')
})
