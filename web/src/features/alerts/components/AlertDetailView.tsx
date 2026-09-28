import { useState, type ReactNode } from 'react'
import { IconArrowLeft, IconBellOff } from '@tabler/icons-react'
import { Link } from 'react-router'
import { Cell, DataTable, HeadCell, HeadRow, Row, RowNumberCell, RowNumberHeadCell } from '@/components/data-table/DataTable'
import { Button } from '@/components/ui/button'
import { InlineError } from '@/components/ui/inline-error'
import { useAuth } from '@/features/auth/hooks/useAuth'
import { formatFullDate } from '@/features/hosts/utils/format'
import { useAlertDetail } from '../hooks/useAlerts'
import type { Alert, Delivery } from '../types/alert.types'
import { canAckAlerts, canResolveAlerts } from '../utils/permissions'
import { exposedPortsText, formatAlertValue, formatMetricValue, isUpdatesKind, KIND_LABELS, METRIC_LABELS, RESOLVE_REASON_LABELS } from '../utils/format'
import { AlertActionDialog, type AlertActionKind } from './AlertActionDialog'
import { AlertStateBadge, EscalatedBadge, FlappingBadge, SeverityBadge } from './AlertBadges'
import { HostEventsList } from './HostEventsList'

const EVENT_LABELS: Record<Delivery['event'], string> = { opened: 'Apertura', resolved: 'Resolución', reminder: 'Recordatorio' }
const DELIVERY_STATUS_LABELS: Record<Delivery['status'], string> = {
  pending: 'Pendiente',
  sending: 'Enviando',
  sent: 'Enviada',
  dead: 'Fallida',
  suppressed: 'Omitida (mantenimiento)',
}

function Item({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-0.5">
      <dt className="text-xs text-text-muted">{label}</dt>
      <dd className="text-sm tabular-nums">{children}</dd>
    </div>
  )
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="flex flex-col gap-3">
      <h2 className="text-sm font-semibold">{title}</h2>
      {children}
    </section>
  )
}

function ruleDescription(a: Alert): string {
  if (a.kind === 'exposed_port') return KIND_LABELS.exposed_port
  if (a.kind === 'host_status') return KIND_LABELS.host_status
  if (isUpdatesKind(a.kind)) return KIND_LABELS[a.kind]
  if (a.kind === 'no_data') return `${KIND_LABELS.no_data}: umbral de ${formatMetricValue(a.kind, a.metric, a.threshold)} sin reportar`
  const metric = a.metric ? METRIC_LABELS[a.metric] : KIND_LABELS.metric
  return `${metric}: umbral de ${formatMetricValue(a.kind, a.metric, a.threshold)}`
}

export function AlertDetailView({ id }: { id: string }) {
  const { user } = useAuth()
  const { data, error, errorStatus, loading, refresh } = useAlertDetail(id)
  const [target, setTarget] = useState<{ action: AlertActionKind; alert: Alert } | null>(null)
  const notFound = errorStatus === 400 || errorStatus === 404
  const a = data?.alert
  const canAck = canAckAlerts(user)
  const canResolve = canResolveAlerts(user)

  return (
    <div className="flex flex-col gap-6" aria-busy={loading}>
      <Link
        to="/alertas"
        className="flex w-fit items-center gap-1 rounded-md text-sm text-text-muted outline-none hover:text-text focus-visible:ring-2 focus-visible:ring-ring"
      >
        <IconArrowLeft className="size-4" stroke={1.5} aria-hidden />
        Volver a alertas
      </Link>

      {notFound ? (
        <InlineError>Alerta no encontrada.</InlineError>
      ) : (
        <>
          {error && <InlineError>{error}</InlineError>}
          {loading && <p className="text-sm text-text-muted">Cargando…</p>}
          {a && data && (
            <>
              <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
                <h1 className="min-w-0 truncate text-lg font-semibold">{a.rule_name}</h1>
                <SeverityBadge severity={a.severity} />
                <AlertStateBadge alert={a} />
                <FlappingBadge flapping={a.flapping} />
                <EscalatedBadge escalatedAt={a.escalated_at} />
                {(canAck || canResolve) && a.state !== 'resolved' && (
                  <span className="ml-auto flex gap-2">
                    {canAck && a.state === 'open' && (
                      <Button variant="outline" size="sm" onClick={() => setTarget({ action: 'ack', alert: a })}>
                        Reconocer
                      </Button>
                    )}
                    {canResolve && (
                      <Button variant="outline" size="sm" onClick={() => setTarget({ action: 'resolve', alert: a })}>
                        Resolver
                      </Button>
                    )}
                  </span>
                )}
              </div>
              {a.escalated_at && <p className="text-sm text-text-muted">Escaló el {formatFullDate(a.escalated_at)}: sin reconocer, subió de severidad.</p>}
              {a.flapping && <p className="text-sm text-text-muted">Se abrió 3 o más veces en 30 minutos. El aviso se retiene mientras oscila y se envía si sigue abierta pasados 30 minutos.</p>}
              {a.suppressed && a.state !== 'resolved' && (
                <p className="text-sm text-text-muted">
                  El host está en una ventana de mantenimiento: la alerta es visible pero no se notifica.
                </p>
              )}

              <dl className="grid gap-4 rounded-md border border-border bg-surface p-4 sm:grid-cols-2 lg:grid-cols-3">
                <Item label="Host">
                  <Link to={`/hosts/${a.host.id}`} className="font-semibold underline">
                    {a.host.hostname}
                  </Link>
                </Item>
                <Item label="Regla al abrir">{ruleDescription(a)}</Item>
                <Item label="Abierta">{formatFullDate(a.opened_at)}</Item>
                {a.kind === 'exposed_port' ? (
                  <Item label="Puertos">{exposedPortsText(a)}</Item>
                ) : isUpdatesKind(a.kind) ? (
                  <Item label="Valor al abrir">{formatAlertValue(a)}</Item>
                ) : (
                  <>
                    <Item label="Valor al abrir">{formatMetricValue(a.kind, a.metric, a.trigger_value)}</Item>
                    <Item label="Valor máximo (pico)">{formatMetricValue(a.kind, a.metric, a.peak_value)}</Item>
                  </>
                )}
                <Item label="Reconocida">
                  {a.acknowledged_at ? `${formatFullDate(a.acknowledged_at)} por ${a.acknowledged_by ?? 'desconocido'}` : '—'}
                </Item>
                <Item label="Resuelta">
                  {a.resolved_at
                    ? `${formatFullDate(a.resolved_at)}${a.resolved_by ? ` por ${a.resolved_by}` : ''}`
                    : '—'}
                </Item>
                {a.resolve_reason && <Item label="Motivo de resolución">{RESOLVE_REASON_LABELS[a.resolve_reason]}</Item>}
              </dl>

              <Section title="Notificaciones">
                {data.deliveries.length === 0 ? (
                  <p className="flex items-center gap-2 text-sm text-text-muted">
                    <IconBellOff className="size-4 shrink-0" stroke={1.5} aria-hidden />
                    Sin notificaciones: esta alerta no tiene entregas (aún no hay canales configurados).
                  </p>
                ) : (
                  <DataTable caption="Entregas de notificaciones" className="min-w-[40rem]">
                    <thead>
                      <HeadRow>
                        <RowNumberHeadCell />
                        <HeadCell>Canal</HeadCell>
                        <HeadCell>Evento</HeadCell>
                        <HeadCell>Estado</HeadCell>
                        <HeadCell>Intentos</HeadCell>
                        <HeadCell>Enviada</HeadCell>
                      </HeadRow>
                    </thead>
                    <tbody>
                      {data.deliveries.map((d, i) => (
                        <Row key={d.id}>
                          <RowNumberCell n={i + 1} />
                          <Cell>{d.channel_name}</Cell>
                          <Cell>{EVENT_LABELS[d.event]}</Cell>
                          <Cell>{DELIVERY_STATUS_LABELS[d.status]}</Cell>
                          <Cell className="tabular-nums">{d.attempts}</Cell>
                          <Cell className="tabular-nums">{d.sent_at ? formatFullDate(d.sent_at) : '—'}</Cell>
                        </Row>
                      ))}
                    </tbody>
                  </DataTable>
                )}
              </Section>

              <Section title="Estado del host">
                <HostEventsList events={data.host_events} />
              </Section>
            </>
          )}
        </>
      )}
      <AlertActionDialog
        target={target}
        onClose={() => {
          setTarget(null)
          void refresh()
        }}
      />
    </div>
  )
}
