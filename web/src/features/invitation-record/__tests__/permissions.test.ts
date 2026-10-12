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
import { describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import type { AuthUser } from '@/stores/auth-store'

import { canReadAllInvitationRecords } from '../lib/permissions'
import { fetchInvitationRecords } from '../lib/utils'

describe('invitation record view permissions', () => {
  test('an admitted administrator can view all users while root retains its privilege', () => {
    expect(
      canReadAllInvitationRecords({
        id: 1,
        username: 'admin',
        role: 10,
        permissions: { admin_permissions: { invitation: { read: true } } },
      })
    ).toBe(true)
    expect(
      canReadAllInvitationRecords({ id: 1, username: 'root', role: 100 })
    ).toBe(true)
  })
  test.each([
    {
      id: 1,
      username: 'admin',
      role: 10,
      permissions: { admin_permissions: { invitation: { read: false } } },
    },
    { id: 1, username: 'user', role: 1 },
  ])(
    'user $username without the all-users grant can still request own records without foreign filters',
    async (user: AuthUser) => {
      const get = vi.spyOn(api, 'get').mockResolvedValue({
        data: { success: true, data: { items: [], total: 0 } },
      })
      const isAdmin = canReadAllInvitationRecords(user)
      expect(isAdmin).toBe(false)
      await fetchInvitationRecords({
        isAdmin,
        page: 1,
        pageSize: 20,
        searchParams: { inviter_name: 'foreign-owner' },
      })
      const url = new URL(String(get.mock.calls[0][0]), 'https://example.test')
      expect(url.pathname).toBe('/api/invitation_record/self')
      expect(url.searchParams.has('inviter_name')).toBe(false)
    }
  )
})
