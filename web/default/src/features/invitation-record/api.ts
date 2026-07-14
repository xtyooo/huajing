import { api } from '@/lib/api'

import type {
  GetInvitationRecordsParams,
  GetInvitationRecordsResponse,
} from './types'

// ============================================================================
// Generic API Helpers
// ============================================================================

function buildApiPath(endpoint: string, isAdmin: boolean): string {
  return isAdmin ? endpoint : `${endpoint}/self`
}

function buildQueryParams(params: Record<string, unknown>): URLSearchParams {
  const queryParams = new URLSearchParams()
  Object.entries(params).forEach(([key, value]) => {
    if (value !== undefined && value !== null && value !== '') {
      queryParams.append(key, String(value))
    }
  })
  return queryParams
}

async function fetchInvitationRecord(
  endpoint: string,
  params: GetInvitationRecordsParams,
  isAdmin: boolean
): Promise<GetInvitationRecordsResponse> {
  const paramRecord = params as unknown as Record<string, unknown>
  const queryParams = buildQueryParams({
    p: paramRecord.p || 1,
    page_size: paramRecord.page_size || 20,
    ...params,
  })
  const path = buildApiPath(endpoint, isAdmin)
  const res = await api.get(`${path}?${queryParams}`)
  return res.data
}

export const getAllInvitationRecords = (params: GetInvitationRecordsParams) =>
  fetchInvitationRecord('/api/invitation_record', params, true)

export const getUserInvitationRecords = (params: GetInvitationRecordsParams) =>
  fetchInvitationRecord('/api/invitation_record', params, false)
