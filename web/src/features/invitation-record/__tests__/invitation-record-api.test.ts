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

import { fetchInvitationRecords } from '../lib/utils'

describe('invitation records request', () => {
  test('administrator filters and millisecond time ranges reach the admin endpoint', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: { items: [], total: 0 } },
    })
    await fetchInvitationRecords({
      isAdmin: true,
      page: 2,
      pageSize: 10,
      searchParams: {
        inviter_name: 'inviter',
        invitee_name: 'invitee',
        startTime: 1700000000000,
        endTime: 1700000100000,
      },
    })
    const url = new URL(String(get.mock.calls[0][0]), 'https://example.test')
    expect(url.pathname).toBe('/api/invitation_record/')
    expect(Object.fromEntries(url.searchParams)).toEqual({
      p: '2',
      page_size: '10',
      inviter_name: 'inviter',
      invitee_name: 'invitee',
      start_timestamp: '1700000000',
      end_timestamp: '1700000100',
    })
  })
  test('regular users use the self endpoint without another inviter override or implicit date filters', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: { items: [], total: 0 } },
    })
    await fetchInvitationRecords({
      isAdmin: false,
      page: 1,
      pageSize: 20,
      searchParams: {
        inviter_name: 'another-user',
        inviter_id: 99,
        invitee_name: 'invitee',
      },
    })
    const url = new URL(String(get.mock.calls[0][0]), 'https://example.test')
    expect(url.pathname).toBe('/api/invitation_record/self')
    expect(Object.fromEntries(url.searchParams)).toEqual({
      p: '1',
      page_size: '20',
      invitee_name: 'invitee',
    })
  })
})
