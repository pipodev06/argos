import { CircleCheckIcon, CircleOffIcon, CopyIcon, PencilIcon, PauseIcon, PlayIcon, Trash2Icon } from 'lucide-react'
import { Cell, DataTable, HeadCell, HeadRow, Row, RowNumberCell, RowNumberHeadCell, TableBody, TableHeader } from '@/components/data-table/DataTable'
import { ActionsHeadCell } from '@/components/data-table/RowActions'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import type { AlertRule } from '../types/alert.types'
import { describeCondition, KIND_LABELS, METRIC_LABELS, scopeLabel } from '../utils/format'
import { EscalationBadge, SeverityBadge } from './AlertBadges'

export type RuleAction = 'edit' | 'clone' | 'toggle' | 'delete'

interface RulesTableProps {
  rules: AlertRule[]
  /** Solo admin ve la columna de acciones. */
  canAct: boolean
  busy: boolean
  onAction: (action: RuleAction, rule: AlertRule) => void
}

export function RuleEnabledBadge({ enabled }: { enabled: boolean }) {
  return enabled ? (
    <Badge Icon={CircleCheckIcon} tint="ok">
      Activa
    </Badge>
  ) : (
    <Badge Icon={CircleOffIcon}>Desactivada</Badge>
  )
}

export function RulesTable({ rules, canAct, busy, onAction }: RulesTableProps) {
  return (
    <DataTable caption="Reglas de alerta" className="min-w-[60rem]">
      <TableHeader>
        <HeadRow>
          <RowNumberHeadCell />
          <HeadCell>Nombre</HeadCell>
          <HeadCell>Tipo</HeadCell>
          <HeadCell>Condición</HeadCell>
          <HeadCell>Ámbito</HeadCell>
          <HeadCell>Severidad</HeadCell>
          <HeadCell>Estado</HeadCell>
          <HeadCell>Alertas abiertas</HeadCell>
          {canAct && <ActionsHeadCell />}
        </HeadRow>
      </TableHeader>
      <TableBody>
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
              <div className="flex flex-col items-center gap-1">
                <RuleEnabledBadge enabled={r.enabled} />
                <EscalationBadge seconds={r.escalate_after_seconds} />
              </div>
            </Cell>
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
                  <PencilIcon strokeWidth={1.5} aria-hidden />
                </Button>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={`Duplicar regla ${r.name}`}
                  title="Duplicar"
                  onClick={() => onAction('clone', r)}
                >
                  <CopyIcon strokeWidth={1.5} aria-hidden />
                </Button>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={`${r.enabled ? 'Desactivar' : 'Activar'} regla ${r.name}`}
                  title={r.enabled ? 'Desactivar' : 'Activar'}
                  disabled={busy}
                  onClick={() => onAction('toggle', r)}
                >
                  {r.enabled ? <PauseIcon strokeWidth={1.5} aria-hidden /> : <PlayIcon strokeWidth={1.5} aria-hidden />}
                </Button>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={`Eliminar regla ${r.name}`}
                  title="Eliminar"
                  onClick={() => onAction('delete', r)}
                >
                  <Trash2Icon strokeWidth={1.5} aria-hidden />
                </Button>
              </Cell>
            )}
          </Row>
        ))}
      </TableBody>
    </DataTable>
  )
}
