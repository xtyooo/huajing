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
  buildNativeMediaPricing,
  nativeMediaPricingDraft,
  preserveNativeMediaPricing,
} from '../native-media-pricing'
import type { PricingValues } from '../pricing'

describe('native pricing alongside versioned model pricing', () => {
  test('saving token price changes retains configured resolution tiers and provider expressions', () => {
    const resolution = { enabled: true, setting: { '720p': 0.12 } }
    const draft: PricingValues = {
      ModelRatio: 2,
      'billing_setting.billing_mode': 'ratio',
      'billing_setting.plugin_billing_expr': { video: '0.2' },
    }
    expect(
      preserveNativeMediaPricing(
        { resolution_price_setting: resolution },
        draft
      )
    ).toEqual({ ...draft, resolution_price_setting: resolution })
  })
  test('request price edits preserve explicit skip_seconds true and false', () => {
    for (const skipSeconds of [true, false]) {
      expect(
        preserveNativeMediaPricing(
          { 'billing_setting.skip_seconds': skipSeconds },
          { ModelPrice: 0.5 }
        )['billing_setting.skip_seconds']
      ).toBe(skipSeconds)
    }
  })
  test('switching to expressions explicitly disables conflicting media pricing without discarding tiers', () => {
    const settings = { enabled: true, setting: { '720p': 0.12 } }
    const result = preserveNativeMediaPricing(
      {
        resolution_price_setting: settings,
        'billing_setting.skip_seconds': true,
      },
      {
        'billing_setting.billing_mode': 'tiered_expr',
        'billing_setting.billing_expr': '1',
      }
    )
    expect(result.resolution_price_setting).toEqual({
      ...settings,
      enabled: false,
    })
    expect(result['billing_setting.skip_seconds']).toBe(false)
  })
  test('resolution price save uses absolute configured tier prices and keeps plugin expressions', () => {
    const result = buildNativeMediaPricing(
      {
        'billing_setting.billing_mode': 'tiered_expr',
        'billing_setting.billing_expr': '1',
        'billing_setting.plugin_billing_expr': { image: '2' },
      },
      { mode: 'resolution', prices: { '720p': '0.123' }, skipSeconds: true }
    )
    expect(result.resolution_price_setting).toEqual({
      enabled: true,
      setting: { '720p': 0.123 },
    })
    expect(result['billing_setting.billing_expr']).toBeUndefined()
    expect(result['billing_setting.skip_seconds']).toBe(false)
    expect(result['billing_setting.plugin_billing_expr']).toEqual({
      image: '2',
    })
  })
  test.each(['0', '-1', 'NaN', 'Infinity', ''])(
    'invalid resolution tier %s cannot become a zero or unbounded price',
    (price) => {
      expect(() =>
        buildNativeMediaPricing(
          {},
          { mode: 'resolution', prices: { '720p': price }, skipSeconds: false }
        )
      ).toThrow()
    }
  )
  test('image size pricing requires all three positive size tiers', () => {
    expect(() =>
      buildNativeMediaPricing(
        {},
        {
          mode: 'image-size',
          prices: { '1k': '1', '2k': '2' },
          skipSeconds: false,
        }
      )
    ).toThrow()
    expect(
      buildNativeMediaPricing(
        {},
        {
          mode: 'image-size',
          prices: { '1k': '1', '2k': '2', '4k': '4' },
          skipSeconds: false,
        }
      ).image_size_price_setting
    ).toEqual({ enabled: true, setting: { '1k': 1, '2k': 2, '4k': 4 } })
  })
  test('historical disabled tiers and explicit false survive a draft round trip', () => {
    expect(
      nativeMediaPricingDraft({
        resolution_price_setting: { enabled: false, setting: { '720p': 1 } },
        'billing_setting.skip_seconds': false,
      }).skipSeconds
    ).toBe(false)
    expect(
      buildNativeMediaPricing(
        {
          resolution_price_setting: { enabled: false, setting: { '720p': 1 } },
        },
        { mode: 'none', prices: {}, skipSeconds: false }
      ).resolution_price_setting?.setting
    ).toEqual({ '720p': 1 })
  })
})
