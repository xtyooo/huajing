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
