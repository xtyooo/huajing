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
import type { PricingValues } from './pricing'

export type NativeMediaPricingDraft = {
  mode: 'none' | 'resolution' | 'image-size'
  prices: Record<string, string>
  skipSeconds: boolean
}

export function nativeMediaPricingDraft(
  values: PricingValues
): NativeMediaPricingDraft {
  let mode: NativeMediaPricingDraft['mode'] = 'none'
  if (values.resolution_price_setting?.enabled) mode = 'resolution'
  else if (values.image_size_price_setting?.enabled) mode = 'image-size'
  const setting =
    mode === 'resolution'
      ? values.resolution_price_setting
      : values.image_size_price_setting
  return {
    mode,
    prices: Object.fromEntries(
      Object.entries(setting?.setting ?? {}).map(([key, value]) => [
        key,
        String(value),
      ])
    ),
    skipSeconds: values['billing_setting.skip_seconds'] === true,
  }
}

export function buildNativeMediaPricing(
  values: PricingValues,
  draft: NativeMediaPricingDraft
): PricingValues {
  const pricing: PricingValues = {
    ...values,
    'billing_setting.skip_seconds': draft.skipSeconds,
  }
  // PATCH replaces a complete draft; retain disabled tier maps for later reuse.
  pricing.resolution_price_setting = {
    ...values.resolution_price_setting,
    enabled: false,
    setting: values.resolution_price_setting?.setting ?? {},
  }
  pricing.image_size_price_setting = {
    ...values.image_size_price_setting,
    enabled: false,
    setting: values.image_size_price_setting?.setting ?? {},
  }
  if (draft.mode !== 'none') {
    const required =
      draft.mode === 'resolution'
        ? ['480p', '720p', '1080p']
        : ['1k', '2k', '4k']
    const entries = Object.entries(draft.prices).filter(
      ([, price]) => price.trim() !== ''
    )
    if (
      !entries.length ||
      (draft.mode === 'image-size' &&
        required.some((key) => !draft.prices[key]?.trim()))
    ) {
      throw new Error('Configure positive prices for the selected media tiers')
    }
    const setting: Record<string, number> = {}
    for (const [key, price] of entries) {
      const value = Number(price)
      if (!Number.isFinite(value) || value <= 0) {
        throw new Error(
          'Configure positive prices for the selected media tiers'
        )
      }
      setting[key] = value
    }
    const key =
      draft.mode === 'resolution'
        ? 'resolution_price_setting'
        : 'image_size_price_setting'
    pricing[key] = { enabled: true, setting }
    pricing['billing_setting.billing_mode'] = 'ratio'
    delete pricing['billing_setting.billing_expr']
    pricing['billing_setting.skip_seconds'] = false
  }
  return pricing
}

export function preserveNativeMediaPricing(
  values: PricingValues,
  pricing: PricingValues
): PricingValues {
  const merged: PricingValues = { ...pricing }
  for (const key of [
    'resolution_price_setting',
    'image_size_price_setting',
  ] as const) {
    if (values[key]) {
      merged[key] = {
        ...values[key],
        enabled:
          pricing['billing_setting.billing_mode'] !== 'tiered_expr' &&
          values[key].enabled,
      }
    }
  }
  if (values['billing_setting.skip_seconds'] !== undefined) {
    merged['billing_setting.skip_seconds'] =
      pricing.ModelPrice !== undefined &&
      pricing['billing_setting.billing_mode'] !== 'tiered_expr' &&
      values['billing_setting.skip_seconds']
  }
  return merged
}
