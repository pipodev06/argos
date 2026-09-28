import { IconBellPause, IconBellRinging2, IconCircleCheck, IconCircleOff, IconCopy, IconPencil, IconPlayerPause, IconPlayerPlay, IconTrash } from '@tabler/icons-react'
import { Cell, DataTable, HeadCell, HeadRow, Row, RowNumberCell, RowNumberHeadCell } from '@/components/data-table/DataTable'
import { Button } from '@/components/ui/button'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import type { AlertRule } from '../types/alert.types'
import { describeCondition, isMuted, KIND_LABELS, METRIC_LABELS, MUTE_OPTIONS, scopeLabel } from '../utils/format'
import { MutedBadge, SeverityBadge } from './AlertBadges'

export type RuleAction = 'edit' | 'clone' | 'toggle' | 'delete'

interface RulesTableProps {
  rules: AlertRule[]
  /** Solo admin ve la columna de acciones. */
  canAct: boolean
  busy: boolean
  onAction: (action: RuleAction, rule: AlertRule) => void
  onMute: (rule: AlertRule, minutes: number) => void
  onUnmute: (rule: AlertRule) => void
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

export function RulesTable({ rules, canAct, busy, onAction, onMute, onUnmute }: RulesTableProps) {
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
              <div className="flex flex-col items-center gap-1">
                <RuleEnabledBadge enabled={r.enabled} />
                {isMuted(r) && r.muted_until && <MutedBadge mutedUntil={r.muted_until} />}
              </div>
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
                {isMuted(r) ? (
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label={`Quitar silencio a la regla ${r.name}`}
                    title="Quitar silencio"
                    disabled={busy}
                    onClick={() => onUnmute(r)}
                  >
                    <IconBellRinging2 stroke={1.5} aria-hidden />
                  </Button>
                ) : (
                  <DropdownMenu modal={false}>
                    <DropdownMenuTrigger asChild>
                      <Button variant="ghost" size="icon-sm" aria-label={`Silenciar regla ${r.name}`} title="Silenciar" disabled={busy}>
                        <IconBellPause stroke={1.5} aria-hidden />
                      </Button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent>
                      {MUTE_OPTIONS.map((opt) => (
                        <DropdownMenuItem key={opt.minutes} onSelect={() => onMute(r, opt.minutes)}>
                          {opt.label}
                        </DropdownMenuItem>
                      ))}
                    </DropdownMenuContent>
                  </DropdownMenu>
                )}
              </Cell>
            )}
          </Row>
        ))}
      </tbody>
    </DataTable>
  )
}
