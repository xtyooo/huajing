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
import { describe, test } from 'node:test'

import { TASK_ACTIONS, TASK_STATUS } from '../../constants'
import { getDownloadableCachedMediaUrl } from '../task-media'

describe('任务缓存媒体下载', () => {
  test('图片和视频成功缓存后都返回统一下载地址', () => {
    const mediaUrl = 'https://example.test/cached-media'

    assert.equal(
      getDownloadableCachedMediaUrl({
        status: TASK_STATUS.SUCCESS,
        action: TASK_ACTIONS.IMAGE_GENERATE,
        media_url: mediaUrl,
      }),
      mediaUrl
    )
    assert.equal(
      getDownloadableCachedMediaUrl({
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
        status: TASK_STATUS.FAILURE,
        action: TASK_ACTIONS.TEXT_GENERATE,
        media_url: 'https://example.test/cached-media',
      }),
      ''
    )
    assert.equal(
      getDownloadableCachedMediaUrl({
        status: TASK_STATUS.SUCCESS,
        action: TASK_ACTIONS.TEXT_GENERATE,
        media_url: 'javascript:alert(1)',
      }),
      ''
    )
  })
})
