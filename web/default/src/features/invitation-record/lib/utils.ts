/**
 * Utility functions for usage logs feature
 */
import {
  getAllInvitationRecords,
  getUserInvitationRecords
} from '../api'
import type {
  FetchInvitationRecordsConfig,
  GetInvitationRecordsParams,
  GetInvitationRecordsResponse
} from '../types'

/**
 * Get default time range (today 00:00:00 to now + 1 hour)
 */
export function getDefaultTimeRange(): { start: Date; end: Date } {
  const now = new Date()
  const start = new Date(now)
  start.setHours(0, 0, 0, 0)
  const end = new Date(now.getTime() + 3600 * 1000) // +1 hour

  return { start, end }
}

/**
 * Convert milliseconds timestamp to seconds for API
 */
function timestampToSeconds(ms: number): number {
  return Math.floor(ms / 1000)
}

/**
 * Build query parameters from filters
 */
export function buildQueryParams(
  params: Record<string, unknown>
): URLSearchParams {
  const queryParams = new URLSearchParams()

  Object.entries(params).forEach(([key, value]) => {
    // Keep 0 as a valid value, only filter out undefined, null, and empty string
    if (value !== undefined && value !== null && value !== '') {
      queryParams.append(key, String(value))
    }
  })

  return queryParams
}

/**
 * Build time range parameters with default values
 * Shared logic for all log types
 */
function buildTimeRangeParams(
  searchParams: Record<string, unknown>
): { start_timestamp?: number; end_timestamp?: number } {
  const hasTimeParams = searchParams.startTime ?? searchParams.endTime
  const defaultTimeRange = !hasTimeParams ? getDefaultTimeRange() : null

  const convertTimestamp = (timestamp: number) => timestampToSeconds(timestamp)

  const getTimestamp = (paramTime?: unknown, defaultTime?: Date) => {
    const time = (paramTime as number) || defaultTime?.getTime()
    return time ? convertTimestamp(time) : undefined
  }

  return {
    start_timestamp: getTimestamp(
      searchParams.startTime,
      defaultTimeRange?.start
    ),
    end_timestamp: getTimestamp(searchParams.endTime, defaultTimeRange?.end),
  }
}

/**
 * Build base parameters with time range (for drawing and task logs)
 * @param useMilliseconds - Whether to use millisecond timestamps (true for drawing logs, false for task logs)
 */
export function buildBaseParams(config: {
  page: number
  pageSize: number
  searchParams: Record<string, unknown>
}): {
  p: number
  page_size: number
  start_timestamp?: number
  end_timestamp?: number
} {
  const { page, pageSize } = config

  return {
    p: page,
    page_size: pageSize,
    start_timestamp: undefined,
    end_timestamp: undefined,
  }

}

/**
 * Build API params from search params and column filters (for common logs)
 */
export function buildApiParams(config: {
  page: number
  pageSize: number
  searchParams: Record<string, unknown>
  isAdmin: boolean
}): GetInvitationRecordsParams {
  const { page, pageSize, searchParams, isAdmin } = config

  // Build base params from search params
  const params: GetInvitationRecordsParams = {
    p: page,
    page_size: pageSize,
    ...(searchParams.invitee_id ? { invitee_id: Number(searchParams.invitee_id) } : {}),
    ...(searchParams.invitee_name ? { invitee_name: String(searchParams.invitee_name) } : {}),
    ...(searchParams.group ? { group: String(searchParams.group) } : {}),
    ...(isAdmin && searchParams.inviter_id
      ? { inviter_id: Number(searchParams.inviter_id) }
      : {}),
    ...(isAdmin && searchParams.inviter_name
      ? { inviter_name: String(searchParams.inviter_name) }
      : {}),
    ...buildTimeRangeParams(searchParams),
  }

  return params
}

// ============================================================================
// Data Fetching
// ============================================================================

/**
 * Fetch logs based on category type
 */
export async function fetchInvitationRecords(
  config: FetchInvitationRecordsConfig
): Promise<GetInvitationRecordsResponse> {
  const { isAdmin, page, pageSize, searchParams } = config

  const baseParams = buildBaseParams({
    page,
    pageSize,
    searchParams
  })

  const paramsWithFilter = {
    ...baseParams
  }

  return isAdmin
    ? await getAllInvitationRecords(paramsWithFilter as GetInvitationRecordsParams)
    : await getUserInvitationRecords(paramsWithFilter as GetInvitationRecordsParams)
}
