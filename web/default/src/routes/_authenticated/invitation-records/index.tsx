import z from 'zod'
import { InvitationRecords } from '@/features/invitation-record'
import { createFileRoute } from '@tanstack/react-router'

const invitationRecordsSearchSchema = z.object({
  page: z.number().optional().catch(1),
  pageSize: z.number().optional().catch(20),
  inviter_name: z.string().optional().catch(''),
  invitee_name: z.string().optional().catch(''),
  startTime: z.number().optional(),
  endTime: z.number().optional(),
})

export const Route = createFileRoute('/_authenticated/invitation-records/')({
  validateSearch: invitationRecordsSearchSchema,
  component: InvitationRecords,
})
