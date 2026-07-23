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

export type ModelPricingMode =
  | 'per-token'
  | 'per-request'
  | 'resolution'
  | 'image-size'

type PricingValues = {
  price?: string
  ratio?: string
  cacheRatio?: string
  completionRatio?: string
  imageRatio?: string
  audioRatio?: string
  audioCompletionRatio?: string
}

type BuildModelPricingMutationInput = {
  mode: ModelPricingMode
  values: PricingValues
  skipSeconds: boolean
  resolutionPrices: Record<'480p' | '720p' | '1080p', string>
  imageSizePrices: Record<'1k' | '2k' | '4k', string>
}

export type ModelPricingMutation = {
  mode: ModelPricingMode
  price?: number
  ratio?: number
  cache_ratio?: number
  completion_ratio?: number
  image_ratio?: number
  audio_ratio?: number
  audio_completion_ratio?: number
  skip_seconds: boolean
  resolution_prices?: Record<string, number>
  image_size_prices?: Record<string, number>
}

const numberOrUndefined = (value?: string) =>
  value === undefined || value.trim() === '' ? undefined : Number(value)

const parsePriceMap = (prices: Record<string, string>) =>
  Object.fromEntries(
    Object.entries(prices)
      .filter(([, value]) => value !== '')
      .map(([key, value]) => [key, Number(value)])
  )

export function buildModelPricingMutation({
  mode,
  values,
  skipSeconds,
  resolutionPrices,
  imageSizePrices,
}: BuildModelPricingMutationInput): ModelPricingMutation {
  const pricing: ModelPricingMutation = {
    mode,
    skip_seconds: mode === 'per-request' && skipSeconds,
  }

  if (mode === 'per-request') {
    pricing.price = numberOrUndefined(values.price)
  } else if (mode === 'per-token') {
    pricing.ratio = numberOrUndefined(values.ratio)
    pricing.cache_ratio = numberOrUndefined(values.cacheRatio)
    pricing.completion_ratio = numberOrUndefined(values.completionRatio)
    pricing.image_ratio = numberOrUndefined(values.imageRatio)
    pricing.audio_ratio = numberOrUndefined(values.audioRatio)
    pricing.audio_completion_ratio = numberOrUndefined(
      values.audioCompletionRatio
    )
  } else if (mode === 'resolution') {
    pricing.resolution_prices = parsePriceMap(resolutionPrices)
  } else if (mode === 'image-size') {
    pricing.image_size_prices = parsePriceMap(imageSizePrices)
  }

  return pricing
}
