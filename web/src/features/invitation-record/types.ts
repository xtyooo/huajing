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
export interface InvitationRecord {
  id: number
  inviter_id: number
  inviter_name: string
  invitee_id: number
  invitee_name: string
  recharge_total: number
  rebate_total: number
  created_at: number
}

export interface FetchInvitationRecordsConfig {
  isAdmin: boolean
  page: number
  pageSize: number
  searchParams: Record<string, unknown>
}

export interface GetInvitationRecordsResponse {
  success: boolean
  message?: string
  data?: {
    items: InvitationRecord[]
    total: number
    page: number
    page_size: number
  }
}

export interface GetInvitationRecordsParams {
  p?: number
  page_size?: number
  inviter_id?: number
  inviter_name?: string
  invitee_id?: number
  invitee_name?: string
  start_timestamp?: number
  end_timestamp?: number
}
