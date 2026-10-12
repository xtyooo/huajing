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
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'

import {
  nativeMediaPricingDraft,
  type NativeMediaPricingDraft,
} from './native-media-pricing'
import type { PricingValues } from './pricing'

export function NativeMediaPricingEditor(props: {
  values: PricingValues
  disabled: boolean
  onSave: (draft: NativeMediaPricingDraft) => Promise<void>
  onDirtyChange?: (dirty: boolean) => void
}) {
  const { t } = useTranslation()
  const onDirtyChange = props.onDirtyChange
  const initial = nativeMediaPricingDraft(props.values)
  const [draft, setDraft] = useState(initial)
  const [error, setError] = useState('')
  const dirty = JSON.stringify(draft) !== JSON.stringify(initial)
  useEffect(() => onDirtyChange?.(dirty), [dirty, onDirtyChange])
  const tiers =
    draft.mode === 'resolution' ? ['480p', '720p', '1080p'] : ['1k', '2k', '4k']
  const save = async () => {
    setError('')
    try {
      await props.onSave(draft)
    } catch (failure) {
      setError(
        failure instanceof Error ? t(failure.message) : t('Operation failed')
      )
    }
  }
  return (
    <section
      className='space-y-3 border-b p-4'
      aria-label={t('Native media pricing')}
    >
      <Label>{t('Native media pricing')}</Label>
      <Select
        value={draft.mode}
        onValueChange={(value) => {
          const mode = value as NativeMediaPricingDraft['mode']
          const setting =
            mode === 'resolution'
              ? props.values.resolution_price_setting
              : props.values.image_size_price_setting
          setDraft({
            ...draft,
            mode,
            prices: Object.fromEntries(
              Object.entries(setting?.setting ?? {}).map(([key, price]) => [
                key,
                String(price),
              ])
            ),
          })
        }}
        disabled={props.disabled}
      >
        <SelectTrigger aria-label={t('Native media pricing mode')}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value='none'>
            {t('Use token or request pricing')}
          </SelectItem>
          <SelectItem value='resolution'>
            {t('Resolution price per second')}
          </SelectItem>
          <SelectItem value='image-size'>
            {t('Image size price per request')}
          </SelectItem>
        </SelectContent>
      </Select>
      {draft.mode !== 'none' && (
        <>
          <p className='text-muted-foreground text-xs'>
            {t(
              'Enabling media tier prices replaces the model billing expression.'
            )}
          </p>
          <div className='grid grid-cols-3 gap-3'>
            {tiers.map((tier) => (
              <div key={tier} className='space-y-1'>
                <Label htmlFor={`media-price-${tier}`}>{tier}</Label>
                <Input
                  id={`media-price-${tier}`}
                  type='number'
                  min='0'
                  step='any'
                  value={draft.prices[tier] ?? ''}
                  disabled={props.disabled}
                  onChange={(event) =>
                    setDraft({
                      ...draft,
                      prices: { ...draft.prices, [tier]: event.target.value },
                    })
                  }
                />
              </div>
            ))}
          </div>
        </>
      )}
      {draft.mode === 'none' && props.values.ModelPrice !== undefined && (
        <div className='flex items-center gap-2'>
          <Switch
            id='native-skip-seconds'
            checked={draft.skipSeconds}
            disabled={props.disabled}
            onCheckedChange={(skipSeconds) =>
              setDraft({ ...draft, skipSeconds })
            }
          />
          <Label htmlFor='native-skip-seconds'>
            {t('Skip seconds multiplier for per-request pricing')}
          </Label>
        </div>
      )}
      {error && (
        <p role='alert' className='text-destructive text-sm'>
          {error}
        </p>
      )}
      <Button
        type='button'
        disabled={props.disabled || !dirty}
        onClick={() => void save()}
      >
        {t('Save media pricing')}
      </Button>
    </section>
  )
}
