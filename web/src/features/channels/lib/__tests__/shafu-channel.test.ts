/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
import { describe, test } from 'bun:test'
import assert from 'node:assert/strict'

import { CHANNEL_TYPES, CHANNEL_TYPE_SHAFU } from '../../constants'
import { getChannelTypeConfig } from '../channel-type-config'

describe('shafu channel configuration', () => {
  test('exposes the provider default and fixed video models', () => {
    const config = getChannelTypeConfig(CHANNEL_TYPE_SHAFU)

    assert.equal(CHANNEL_TYPES[CHANNEL_TYPE_SHAFU], 'shafu')
    assert.equal(config.defaultBaseUrl, 'https://shafu.it.com')
    assert.deepEqual(config.supportedModels, [
      'sd-480p',
      'sd-720p',
      'sd-1080p',
      'sdf-480p',
      'sdf-720p',
    ])
    assert.equal(config.hints?.key, 'Bearer NewAPI token')
  })
})
