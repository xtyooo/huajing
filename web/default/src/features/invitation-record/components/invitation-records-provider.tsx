import React, { useState } from 'react'

import type { InvitationRecord } from '../types'

type InvitationRecordsContextType = {
  currentRow: InvitationRecord | null
  setCurrentRow: React.Dispatch<React.SetStateAction<InvitationRecord | null>>
  refreshTrigger: number
  triggerRefresh: () => void
}

const InvitationRecordsContext =
  React.createContext<InvitationRecordsContextType | null>(null)

export function InvitationRecordsProvider({
  children,
}: {
  children: React.ReactNode
}) {
  const [currentRow, setCurrentRow] = useState<InvitationRecord | null>(null)
  const [refreshTrigger, setRefreshTrigger] = useState(0)

  const triggerRefresh = () => setRefreshTrigger((prev) => prev + 1)

  return (
    <InvitationRecordsContext
      value={{
        currentRow,
        setCurrentRow,
        refreshTrigger,
        triggerRefresh,
      }}
    >
      {children}
    </InvitationRecordsContext>
  )
}

// eslint-disable-next-line react-refresh/only-export-components
export const useInvitationRecords = () => {
  const invitationRecordsContext = React.useContext(InvitationRecordsContext)

  if (!invitationRecordsContext) {
    throw new Error(
      'useInvitationRecords has to be used within <InvitationRecordsContext>'
    )
  }

  return invitationRecordsContext
}
