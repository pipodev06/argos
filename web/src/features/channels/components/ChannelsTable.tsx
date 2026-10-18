import { PencilIcon, PauseIcon, PlayIcon, SendIcon, Trash2Icon } from 'lucide-react'
import { Cell, DataTable, HeadCell, HeadRow, Row, RowNumberCell, RowNumberHeadCell, TableBody, TableHeader } from '@/components/data-table/DataTable'
import { ActionsHeadCell } from '@/components/data-table/RowActions'
import { Button } from '@/components/ui/button'
import { SeverityBadge } from '@/features/alerts/components/AlertBadges'
import { RuleEnabledBadge } from '@/features/alerts/components/RulesTable'
import type { TestOutcome } from '../hooks/useChannelTest'
import { ownerLabel } from '../utils/owner'
import type { Channel } from '../types/channel.types'

export type ChannelAction = 'edit' | 'toggle' | 'delete' | 'test'

interface ChannelsTableProps {
  channels: Channel[]
  /** Solo admin ve destino y acciones. */
  canAct: boolean
  busy: boolean
  testingId: string | null
  outcomes: Record<string, TestOutcome>
  onAction: (action: ChannelAction, channel: Channel) => void
}

function testText(id: string, testingId: string | null, outcome: TestOutcome | undefined): string {
  if (testingId === id) return 'Probando...'
  if (!outcome) return ''
  return outcome.ok ? outcome.message : `Falló: ${outcome.message}`
}

export function ChannelsTable({ channels, canAct, busy, testingId, outcomes, onAction }: ChannelsTableProps) {
  return (
    <DataTable caption="Canales de notificación" className="min-w-[56rem]">
      <TableHeader>
        <HeadRow>
          <RowNumberHeadCell />
          <HeadCell>Nombre</HeadCell>
          <HeadCell>Dueño</HeadCell>
          {canAct && <HeadCell>Destino</HeadCell>}
          <HeadCell>Severidad mínima</HeadCell>
          <HeadCell>Estado</HeadCell>
          {canAct && <ActionsHeadCell />}
        </HeadRow>
      </TableHeader>
      <TableBody>
        {channels.map((c, i) => (
          <Row key={c.id}>
            <RowNumberCell n={i + 1} />
            <Cell className="max-w-56 truncate font-semibold">{c.name}</Cell>
            <Cell className="max-w-40 truncate">{ownerLabel(c)}</Cell>
            {canAct && <Cell className="max-w-56 truncate">{c.config?.host ?? '—'}</Cell>}
            <Cell>
              <SeverityBadge severity={c.min_severity} />
            </Cell>
            <Cell>
              <RuleEnabledBadge enabled={c.enabled} />
            </Cell>
            {canAct && (
              <Cell>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={`Probar canal ${c.name}`}
                  title="Probar"
                  disabled={testingId !== null}
                  onClick={() => onAction('test', c)}
                >
                  <SendIcon strokeWidth={1.5} aria-hidden />
                </Button>
                <Button variant="ghost" size="icon-sm" aria-label={`Editar canal ${c.name}`} title="Editar" onClick={() => onAction('edit', c)}>
                  <PencilIcon strokeWidth={1.5} aria-hidden />
                </Button>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={`${c.enabled ? 'Desactivar' : 'Activar'} canal ${c.name}`}
                  title={c.enabled ? 'Desactivar' : 'Activar'}
                  disabled={busy}
                  onClick={() => onAction('toggle', c)}
                >
                  {c.enabled ? <PauseIcon strokeWidth={1.5} aria-hidden /> : <PlayIcon strokeWidth={1.5} aria-hidden />}
                </Button>
                <Button variant="ghost" size="icon-sm" aria-label={`Eliminar canal ${c.name}`} title="Eliminar" onClick={() => onAction('delete', c)}>
                  <Trash2Icon strokeWidth={1.5} aria-hidden />
                </Button>
                <span role="status" className="block text-xs text-text-muted">
                  {testText(c.id, testingId, outcomes[c.id])}
                </span>
              </Cell>
            )}
          </Row>
        ))}
      </TableBody>
    </DataTable>
  )
}
