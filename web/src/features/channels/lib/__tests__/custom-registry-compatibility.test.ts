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
import { describe, expect, test } from 'vitest'

import {
  CHANNEL_TYPES,
  CHANNEL_TYPE_OPTIONS,
  CHANNEL_TYPE_SUB2API,
  CHANNEL_TYPE_NEW_API,
  CHANNEL_TYPE_TASK_PLUGIN,
  CHANNEL_TYPE_VLLM,
  CHANNEL_TYPE_SGLANG,
  channelTypeOptionsForTaskPluginBind,
  MODEL_FETCHABLE_TYPES,
} from '../../constants'
import { CHANNEL_TYPE_ADVANCED_CUSTOM } from '../advanced-custom'
import { getChannelTypeConfig } from '../channel-type-config'

describe('persisted custom channel type compatibility', () => {
  test('all 24 existing types retain their provider labels and stay selectable', () => {
    const labels = [
      'MuseAI',
      'MeAI',
      'Xs',
      'HJ',
      'Mimo',
      'Lingjing',
      'Advanced Custom',
      'kuai',
      'sd0717',
      'wufan',
      'Sub2API',
      'New API',
      '安和',
      'zhou_sd',
      'AutoDL H3',
      'diaomao',
      'shafu',
      '满聚',
      'naonao',
      'manying',
      'wanchen',
      'yaochen',
      'longxia6',
      'snumom',
    ]
    for (const [offset, label] of labels.entries()) {
      const type = offset + 58
      expect(CHANNEL_TYPES[type as keyof typeof CHANNEL_TYPES]).toBe(label)
      expect(CHANNEL_TYPE_OPTIONS).toContainEqual({ value: type, label })
    }
  })
  test('plugin filtering removes only the new plugin type and keeps HJ available', () => {
    expect(CHANNEL_TYPE_TASK_PLUGIN).toBe(82)
    const options = channelTypeOptionsForTaskPluginBind(false)
    expect(options.some((item) => item.value === 82)).toBe(false)
    expect(options).toContainEqual({ value: 61, label: 'HJ' })
  })
  test('upstream additions use noncolliding IDs while gateway IDs retain their semantics', () => {
    expect([
      CHANNEL_TYPE_ADVANCED_CUSTOM,
      CHANNEL_TYPE_SUB2API,
      CHANNEL_TYPE_NEW_API,
      CHANNEL_TYPE_VLLM,
      CHANNEL_TYPE_SGLANG,
    ]).toEqual([64, 68, 69, 83, 84])
    expect(getChannelTypeConfig(83).name).toBe('vLLM')
    expect(getChannelTypeConfig(84).name).toBe('SGLang')
    expect(MODEL_FETCHABLE_TYPES.has(83)).toBe(true)
    expect(MODEL_FETCHABLE_TYPES.has(84)).toBe(true)
  })
})
