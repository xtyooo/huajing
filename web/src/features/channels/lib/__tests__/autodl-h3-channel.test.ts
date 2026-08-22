/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { describe, test } from 'bun:test'
import assert from 'node:assert/strict'

import { CHANNEL_TYPES, CHANNEL_TYPE_AUTODL_H3 } from '../../constants'
import { getChannelTypeConfig } from '../channel-type-config'

describe('AutoDL H3 channel configuration', () => {
  test('exposes provider defaults and exact workflow models', () => {
    const config = getChannelTypeConfig(CHANNEL_TYPE_AUTODL_H3)

    assert.equal(CHANNEL_TYPES[CHANNEL_TYPE_AUTODL_H3], 'AutoDL H3')
    assert.equal(config.defaultBaseUrl, 'https://autodl.art')
    assert.deepEqual(config.supportedModels, [
      'minimax_h3_lightx2v_no_pic',
      'minimax_h3_image_audio_to_video_v2_15s',
    ])
    assert.equal(config.hints?.key, 'Raw AutoDL ComfyUI token')
  })
})
