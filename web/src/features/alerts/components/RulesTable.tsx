import { IconCircleCheck, IconCircleOff, IconCopy, IconPencil, IconPlayerPause, IconPlayerPlay, IconTrash } from '@tabler/icons-react'
import { Cell, DataTable, HeadCell, HeadRow, Row, RowNumberCell, RowNumberHeadCell } from '@/components/data-table/DataTable'
import { Button } from '@/components/ui/button'
import type { AlertRule } from '../types/alert.types'
import { describeCondition, KIND_LABELS, METRIC_LABELS, scopeLabel } from '../utils/format'
import { SeverityBadge } from './AlertBadges'

export type RuleAction = 'edit' | 'clone' | 'toggle' | 'delete'

interface RulesTableProps {
  rules: AlertRule[]
  /** Solo admin ve la columna de acciones. */
  canAct: boolean
  busy: boolean
  onAction: (action: RuleAction, rule: AlertRule) => void
}

export function RuleEnabledBadge({ enabled }: { enabled: boolean }) {
  const Icon = enabled ? IconCircleCheck : IconCircleOff
  return (
    <span
      className={`inline-flex items-center gap-1 rounded-md px-2 py-0.5 text-xs font-medium text-text ${enabled ? 'bg-ok/10' : 'bg-text-muted/10'}`}
    >
      <Icon className={`size-3.5 shrink-0 ${enabled ? 'text-ok' : 'text-text-muted'}`} stroke={1.5} aria-hidden />
      {enabled ? 'Activa' : 'Desactivada'}
    </span>
  )
}

export function RulesTable({ rules, canAct, busy, onAction }: RulesTableProps) {
  return (
    <DataTable caption="Reglas de alerta" className="min-w-[60rem]">
      <thead>
        <HeadRow>
          <RowNumberHeadCell />
          <HeadCell>Nombre</HeadCell>
          <HeadCell>Tipo</HeadCell>
          <HeadCell>Condición</HeadCell>
          <HeadCell>Ámbito</HeadCell>
          <HeadCell>Severidad</HeadCell>
          <HeadCell>Estado</HeadCell>
          <HeadCell>Canales</HeadCell>
          <HeadCell>Alertas abiertas</HeadCell>
          {canAct && <HeadCell>Acciones</HeadCell>}
        </HeadRow>
      </thead>
      <tbody>
        {rules.map((r, i) => (
          <Row key={r.id}>
            <RowNumberCell n={i + 1} />
            <Cell className="max-w-56 truncate font-semibold">{r.name}</Cell>
            <Cell>{r.metric ? `${KIND_LABELS[r.kind]} · ${METRIC_LABELS[r.metric]}` : KIND_LABELS[r.kind]}</Cell>
            <Cell className="tabular-nums">{describeCondition(r)}</Cell>
            <Cell className="max-w-48 truncate">{scopeLabel(r.scope)}</Cell>
            <Cell>
              <SeverityBadge severity={r.severity} />
            </Cell>
            <Cell>
              <RuleEnabledBadge enabled={r.enabled} />
            </Cell>
            <Cell className="tabular-nums">{r.channel_ids.length}</Cell>
            <Cell className="tabular-nums">{r.active_alerts}</Cell>
            {canAct && (
              <Cell>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={`Editar regla ${r.name}`}
                  title="Editar"
                  onClick={() => onAction('edit', r)}
                >
                  <IconPencil stroke={1.5} aria-hidden />
                </Button>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={`Duplicar regla ${r.name}`}
                  title="Duplicar"
                  onClick={() => onAction('clone', r)}
                >
                  <IconCopy stroke={1.5} aria-hidden />
                </Button>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={`${r.enabled ? 'Desactivar' : 'Activar'} regla ${r.name}`}
                  title={r.enabled ? 'Desactivar' : 'Activar'}
                  disabled={busy}
                  onClick={() => onAction('toggle', r)}
                >
                  {r.enabled ? <IconPlayerPause stroke={1.5} aria-hidden /> : <IconPlayerPlay stroke={1.5} aria-hidden />}
                </Button>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={`Eliminar regla ${r.name}`}
                  title="Eliminar"
                  onClick={() => onAction('delete', r)}
                >
                  <IconTrash stroke={1.5} aria-hidden />
                </Button>
              </Cell>
            )}
          </Row>
        ))}
      </tbody>
    </DataTable>
  )
}
