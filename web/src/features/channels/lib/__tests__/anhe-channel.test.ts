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

import { CHANNEL_TYPES, CHANNEL_TYPE_ANHE } from '../../constants'
import { getChannelTypeConfig } from '../channel-type-config'

describe('Anhe channel configuration', () => {
  test('exposes the provider defaults and supported models', () => {
    const config = getChannelTypeConfig(CHANNEL_TYPE_ANHE)

    assert.equal(CHANNEL_TYPES[CHANNEL_TYPE_ANHE], '安和')
    assert.equal(config.defaultBaseUrl, 'https://anhedean.cn')
    assert.deepEqual(config.supportedModels, [
      'seedance-2-5-480p',
      'seedance-2-5-720p',
    ])
  })
})
