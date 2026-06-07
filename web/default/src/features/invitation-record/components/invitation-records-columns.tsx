import { type ColumnDef } from '@tanstack/react-table'
import { useTranslation } from 'react-i18next'
import { formatTimestamp } from '@/lib/format'
import { Checkbox } from '@/components/ui/checkbox'
import { DataTableColumnHeader } from '@/components/data-table'
import { LongText } from '@/components/long-text'
import { type InvitationRecord } from '../data/schema'

export function useInvitationRecordsColumns(isAdmin: boolean): ColumnDef<InvitationRecord>[] {
  const { t } = useTranslation()

  const columns: ColumnDef<InvitationRecord>[] = [
    {
      id: 'select',
      header: ({ table }) => (
        <Checkbox
          checked={
            table.getIsAllPageRowsSelected()
          }
          indeterminate={table.getIsSomePageRowsSelected()}
          onCheckedChange={(value) => table.toggleAllPageRowsSelected(!!value)}
          aria-label='Select all'
          className='translate-y-[2px]'
        />
      ),
      cell: ({ row }) => (
        <Checkbox
          checked={row.getIsSelected()}
          onCheckedChange={(value) => row.toggleSelected(!!value)}
          aria-label='Select row'
          className='translate-y-[2px]'
        />
      ),
      enableSorting: false,
      enableHiding: false,
      meta: { label: t('Select') },
    },
    {
      accessorKey: 'id',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title='ID' />
      ),
      cell: ({ row }) => {
        return <div className='w-[60px]'>{row.getValue('id')}</div>
      },
      enableColumnFilter: false,
      enableMultiSort: false,
      enableGlobalFilter: false,
      enableGrouping: false,
      enablePinning: false,
      enableResizing: false,
      enableSorting:false,
      enableHiding: false,
      meta: { label: t('ID'), mobileHidden: true },
    },
  ]

  if (isAdmin) {
    columns.push({
      accessorKey: 'inviter_name',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={'邀请人'} />
      ),
      cell: ({ row }) => {
        const username = row.getValue('inviter_name') as string

        return (
          <div className='flex min-w-[160px] flex-col gap-1'>
            <div className='flex items-center gap-2'>
              <LongText className='max-w-[140px] font-medium'>
                {username}
              </LongText>
            </div>
          </div>
        )
      },
      enableColumnFilter: false,
      enableMultiSort: false,
      enableGlobalFilter: false,
      enableGrouping: false,
      enablePinning: false,
      enableResizing: false,
      enableSorting:false,
      enableHiding: false,
      meta: { label: '邀请人', mobileTitle: true },
    })
  }

  columns.push({
    accessorKey: 'invitee_name',
    header: ({ column }) => (
      <DataTableColumnHeader column={column} title={'被邀请人'} />
    ),
    cell: ({ row }) => {
      const username = row.getValue('invitee_name') as string

      return (
        <div className='flex min-w-[160px] flex-col gap-1'>
          <div className='flex items-center gap-2'>
            <LongText className='max-w-[140px] font-medium'>
              {username}
            </LongText>
          </div>
        </div>
      )
    },
    enableColumnFilter: false,
    enableMultiSort: false,
    enableGlobalFilter: false,
    enableGrouping: false,
    enablePinning: false,
    enableResizing: false,
    enableSorting:false,
    enableHiding: false,
    meta: { label: '被邀请人', mobileTitle: true },
  })

  columns.push(
    {
      accessorKey: 'created_at',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={'邀请时间'} />
      ),
      cell: ({ row }) => {
        const ts = row.getValue('created_at') as number | undefined
        return (
          <span className='text-muted-foreground text-sm'>
            {ts ? formatTimestamp(ts) : '-'}
          </span>
        )
      },
      enableColumnFilter: false,
      enableMultiSort: false,
      enableGlobalFilter: false,
      enableGrouping: false,
      enablePinning: false,
      enableResizing: false,
      enableSorting:false,
      enableHiding: false,
      meta: { label: '邀请时间', mobileHidden: true },
    }
  )

  columns.push(
    {
      accessorKey: 'recharge_total',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={'总充值金额'} />
      ),
      cell: ({ row }) => {
        const ts = row.getValue('recharge_total') as number | undefined
        return (
          <span className='text-muted-foreground text-sm'>
            {ts || '0'}
          </span>
        )
      },
      enableColumnFilter: false,
      enableMultiSort: false,
      enableGlobalFilter: false,
      enableGrouping: false,
      enablePinning: false,
      enableResizing: false,
      enableSorting:false,
      enableHiding: false,
      meta: { label: '总充值金额', mobileHidden: true },
    }
  )

  columns.push(
    {
      accessorKey: 'rebate_total',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={'总返点金额'} />
      ),
      cell: ({ row }) => {
        const ts = row.getValue('rebate_total') as number | undefined
        return (
          <span className='text-muted-foreground text-sm'>
            {ts || '0'}
          </span>
        )
      },
      enableColumnFilter: false,
      enableMultiSort: false,
      enableGlobalFilter: false,
      enableGrouping: false,
      enablePinning: false,
      enableResizing: false,
      enableSorting:false,
      enableHiding: false,
      meta: { label: '总返点金额', mobileHidden: true },
    }
  )

  return columns
}
