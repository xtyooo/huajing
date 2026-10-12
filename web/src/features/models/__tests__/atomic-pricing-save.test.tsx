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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { ModelMutateDrawer } from '../components/drawers/model-mutate-drawer'
import type { Model } from '../types'

const model: Model = {
  id: 7,
  model_name: 'public-video',
  description: '',
  status: 1,
  sync_official: 1,
  name_rule: 0,
  vendor_id: 0,
  endpoints: '',
  created_time: 1,
  updated_time: 1,
}

afterEach(() => useAuthStore.getState().auth.reset())

function prepare(role: number, grant = true) {
  useAuthStore.getState().auth.setUser({
    id: 1,
    username: 'administrator',
    role,
    permissions: { admin_permissions: { model_pricing: { write: grant } } },
  })
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/models/7') return { data: { success: true, data: model } }
    if (url === '/api/models/pricing') {
      return {
        data: {
          success: true,
          data: [
            {
              key: 'ModelPrice',
              value: JSON.stringify({ 'public-video': 0.2 }),
            },
            {
              key: 'billing_setting.skip_seconds',
              value: JSON.stringify({ 'public-video': true }),
            },
          ],
        },
      }
    }
    return { data: { success: true, data: { items: [] } } }
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const close = vi.fn()
  render(
    <QueryClientProvider client={client}>
      <ModelMutateDrawer open onOpenChange={close} currentRow={model} />
    </QueryClientProvider>
  )
  return { client, close }
}

describe('atomic model and pricing entry alongside metadata tabs', () => {
  test('admitted administrator can rename metadata and save existing request pricing with one atomic API call', async () => {
    const post = vi
      .spyOn(api, 'post')
      .mockResolvedValue({ data: { success: true, data: model } })
    const { client, close } = prepare(10)
    fireEvent.click(
      await screen.findByRole('button', {
        name: 'Save model and pricing together',
      })
    )
    const name = await screen.findByLabelText(/^Model Name/)
    await screen.findByDisplayValue('0.2')
    fireEvent.change(name, { target: { value: 'renamed-public-video' } })
    fireEvent.click(screen.getByRole('button', { name: 'Update Model' }))
    await waitFor(() =>
      expect(post).toHaveBeenCalledWith(
        '/api/models/save',
        expect.objectContaining({
          model: expect.objectContaining({
            id: 7,
            model_name: 'renamed-public-video',
          }),
          old_model_name: 'public-video',
          pricing: { mode: 'per-request', price: 0.2, skip_seconds: true },
        })
      )
    )
    await waitFor(() => expect(close).toHaveBeenCalledWith(false))
    client.clear()
  })
  test('administrators with an explicit pricing denial retain metadata editing without an atomic save entry', async () => {
    const { client } = prepare(10, false)
    expect(
      await screen.findByRole('tab', { name: 'Model metadata' })
    ).toBeVisible()
    expect(
      screen.queryByRole('button', { name: 'Save model and pricing together' })
    ).not.toBeInTheDocument()
    client.clear()
  })
})
