import { SectionPageLayout } from '@/components/layout'

import { InvitationRecordsProvider } from './components/invitation-records-provider'
import { InvitationRecordsTable } from './components/invitation-records-table'

function InvitationRecordsContent() {
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>邀请记录</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <InvitationRecordsTable />
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

export function InvitationRecords() {
  return (
    <InvitationRecordsProvider>
      <InvitationRecordsContent />
    </InvitationRecordsProvider>
  )
}
