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
import { act, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { InvitationRecordsTable } from '../components/invitation-records-table'

const routeState = vi.hoisted(() => ({
  search: { page: 1, pageSize: 20 },
  navigate: vi.fn(),
}))

vi.mock('@tanstack/react-router', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@tanstack/react-router')>()),
  getRouteApi: () => ({
    useSearch: () => routeState.search,
    useNavigate: () => routeState.navigate,
  }),
}))

afterEach(() => useAuthStore.getState().auth.reset())

test('revoking all-users invitation access hides cached foreign rows before self records arrive', async () => {
  const user = { id: 1, username: 'admin', role: 10 }
  useAuthStore.getState().auth.setUser({
    ...user,
    permissions: { admin_permissions: { invitation: { read: true } } },
  })
  let resolveSelf!: (value: Awaited<ReturnType<typeof api.get>>) => void
  const self = new Promise<Awaited<ReturnType<typeof api.get>>>((resolve) => {
    resolveSelf = resolve
  })
  const row = {
    id: 1,
    inviter_id: 2,
    inviter_name: 'another-owner',
    invitee_id: 3,
    invitee_name: 'foreign-invitee',
    recharge_total: 0,
    rebate_total: 0,
    created_at: 1,
  }
  const get = vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (String(url).startsWith('/api/invitation_record/self')) return self
    return { data: { success: true, data: { items: [row], total: 1 } } }
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <InvitationRecordsTable />
    </QueryClientProvider>
  )
  await waitFor(() => expect(screen.getByText('foreign-invitee')).toBeVisible())
  act(() =>
    useAuthStore.getState().auth.setUser({
      ...user,
      permissions: { admin_permissions: { invitation: { read: false } } },
    })
  )
  expect(screen.queryByText('foreign-invitee')).not.toBeInTheDocument()
  expect(screen.queryByText('another-owner')).not.toBeInTheDocument()
  await waitFor(() =>
    expect(
      get.mock.calls.some(([url]) =>
        String(url).startsWith('/api/invitation_record/self')
      )
    ).toBe(true)
  )
  await act(async () =>
    resolveSelf({
      data: {
        success: true,
        data: {
          items: [
            {
              ...row,
              inviter_id: 1,
              inviter_name: 'admin',
              invitee_name: 'own-invitee',
            },
          ],
          total: 1,
        },
      },
    })
  )
  await waitFor(() => expect(screen.getByText('own-invitee')).toBeVisible())
  client.clear()
})
