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
import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import { NativeMediaPricingEditor } from '../native-media-pricing-editor'

describe('native media price editor', () => {
  test('configured tiers remain editable and save as native resolution prices', async () => {
    const save = vi.fn().mockResolvedValue(undefined)
    render(
      <NativeMediaPricingEditor
        values={{
          resolution_price_setting: {
            enabled: true,
            setting: { '720p': 0.12 },
          },
        }}
        disabled={false}
        onSave={save}
      />
    )
    fireEvent.change(screen.getByLabelText('720p'), {
      target: { value: '0.24' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Save media pricing' }))
    expect(save).toHaveBeenCalledWith({
      mode: 'resolution',
      prices: { '720p': '0.24' },
      skipSeconds: false,
    })
  })
  test('a per-request price exposes an explicit skip_seconds switch', () => {
    render(
      <NativeMediaPricingEditor
        values={{ ModelPrice: 0.5, 'billing_setting.skip_seconds': true }}
        disabled={false}
        onSave={vi.fn()}
      />
    )
    expect(
      screen.getByRole('switch', {
        name: 'Skip seconds multiplier for per-request pricing',
      })
    ).toBeChecked()
  })
  test('failed saves show the supplied failure and keep the edited price', async () => {
    render(
      <NativeMediaPricingEditor
        values={{
          resolution_price_setting: {
            enabled: true,
            setting: { '720p': 0.12 },
          },
        }}
        disabled={false}
        onSave={vi.fn().mockRejectedValue(new Error('Price changed; reload'))}
      />
    )
    fireEvent.change(screen.getByLabelText('720p'), {
      target: { value: '0.24' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Save media pricing' }))
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Price changed; reload'
    )
    expect(screen.getByLabelText('720p')).toHaveValue(0.24)
  })
})
