import { Fragment, useState } from 'react'
import { ChevronDownIcon, ChevronRightIcon } from 'lucide-react'
import { Cell, DataTable, HeadCell, HeadRow, Row, RowNumberCell, RowNumberHeadCell, TableBody, TableHeader } from '@/components/data-table/DataTable'
import { rowNumber } from '@/lib/pagination'
import { SkeletonRows } from '@/components/data-table/SkeletonRows'
import type { AuditEntry } from '../types/audit.types'
import { eventLabel, formatAuditDate, formatDetailValue } from '../utils/labels'
import { AuditTarget } from './AuditTarget'
import { OutcomeBadge } from './OutcomeBadge'

const COLS = 7

interface AuditTableProps {
  /** null = cargando. */
  entries: AuditEntry[] | null
  page: number
  pageSize: number
}

export function AuditTable({ entries, page, pageSize }: AuditTableProps) {
  const [openId, setOpenId] = useState<number | null>(null)
  return (
    <DataTable caption="Registro de auditoría" className="min-w-[56rem]">
      <TableHeader>
        <HeadRow>
          <RowNumberHeadCell />
          <HeadCell>Fecha</HeadCell>
          <HeadCell>Evento</HeadCell>
          <HeadCell>Resultado</HeadCell>
          <HeadCell>Actor</HeadCell>
          <HeadCell>Objetivo</HeadCell>
          <HeadCell>IP</HeadCell>
        </HeadRow>
      </TableHeader>
      <TableBody>
        {entries === null && <SkeletonRows colSpan={COLS} />}
        {entries?.map((e, i) => {
          const open = openId === e.id
          const panelId = `audit-detail-${e.id}`
          const Chevron = open ? ChevronDownIcon : ChevronRightIcon
          const detail = Object.entries(e.detail ?? {})
          return (
            <Fragment key={e.id}>
              <Row>
                <RowNumberCell n={rowNumber(page, pageSize, i)} />
                <Cell className="tabular-nums">
                  <button
                    type="button"
                    aria-expanded={open}
                    aria-controls={open ? panelId : undefined}
                    aria-label={`${open ? 'Ocultar' : 'Ver'} detalle de ${eventLabel(e.event)}`}
                    onClick={() => setOpenId(open ? null : e.id)}
                    className="inline-flex items-center gap-1 rounded-md outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  >
                    <Chevron className="size-4 shrink-0 text-text-muted" strokeWidth={1.5} aria-hidden />
                    {formatAuditDate(e.ts)}
                  </button>
                </Cell>
                <Cell>
                  <span title={e.event}>{eventLabel(e.event)}</span>
                </Cell>
                <Cell>
                  <OutcomeBadge outcome={e.outcome} />
                </Cell>
                <Cell>{e.actor_username ?? '—'}</Cell>
                <Cell>
                  <AuditTarget type={e.target_type} id={e.target_id} />
                </Cell>
                <Cell className="tabular-nums">{e.ip ?? '—'}</Cell>
              </Row>
              {open && (
                <tr id={panelId} className="border-b border-border bg-muted/40 last:border-b-0">
                  <td colSpan={COLS} className="px-3 py-3 text-sm">
                    {detail.length === 0 ? (
                      <p className="text-text-muted">Sin detalle.</p>
                    ) : (
                      <dl className="mx-auto grid w-fit max-w-full grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-left">
                        {detail.map(([k, v]) => (
                          <Fragment key={k}>
                            <dt className="text-text-muted">{k}</dt>
                            <dd className="min-w-0 break-words">{formatDetailValue(v)}</dd>
                          </Fragment>
                        ))}
                      </dl>
                    )}
                  </td>
                </tr>
              )}
            </Fragment>
          )
        })}
      </TableBody>
    </DataTable>
  )
}
