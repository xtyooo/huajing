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
