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
import { describe, test } from 'bun:test'
import assert from 'node:assert/strict'

import {
  CHANNEL_TYPES,
  CHANNEL_TYPE_MANYING,
  CHANNEL_TYPE_NAONAO,
} from '../../constants'
import { getChannelTypeConfig } from '../channel-type-config'

describe('naonao and manying channel configuration', () => {
  test('exposes video-only provider defaults and model catalogs', () => {
    const naonao = getChannelTypeConfig(CHANNEL_TYPE_NAONAO)
    const manying = getChannelTypeConfig(CHANNEL_TYPE_MANYING)

    assert.equal(CHANNEL_TYPES[CHANNEL_TYPE_NAONAO], 'naonao')
    assert.equal(naonao.defaultBaseUrl, 'https://gpt.qinnaonao.com')
    assert.deepEqual(naonao.supportedModels, [
      'wan3.0-video',
      'seedance-2.0',
      'seedance-2.0-fast',
      'seedance-2.5',
    ])
    assert.equal(CHANNEL_TYPES[CHANNEL_TYPE_MANYING], 'manying')
    assert.equal(manying.defaultBaseUrl, 'https://shafu.it.com')
    assert.deepEqual(manying.supportedModels, [
      'sd-480p',
      'sd-720p',
      'sd-1080p',
      'sdf-480p',
      'sdf-720p',
    ])
  })
})
