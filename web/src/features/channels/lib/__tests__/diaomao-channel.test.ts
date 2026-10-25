/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
import { describe, test } from 'bun:test'
import assert from 'node:assert/strict'

import { CHANNEL_TYPES, CHANNEL_TYPE_DIAOMAO } from '../../constants'
import { getChannelTypeConfig } from '../channel-type-config'

describe('diaomao channel configuration', () => {
  test('exposes the provider default and sd2-c8 model', () => {
    const config = getChannelTypeConfig(CHANNEL_TYPE_DIAOMAO)

    assert.equal(CHANNEL_TYPES[CHANNEL_TYPE_DIAOMAO], 'diaomao')
    assert.equal(config.defaultBaseUrl, 'https://llm.chre3.com')
    assert.deepEqual(config.supportedModels, ['sd2-c8'])
    assert.equal(config.hints?.key, 'Bearer API key')
  })
})
