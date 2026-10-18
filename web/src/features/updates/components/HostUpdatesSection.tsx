import { lazy, Suspense, useCallback, useEffect, useState } from 'react'
import { CircleCheckIcon, Clock4Icon, ExternalLinkIcon, RotateCwIcon, ShieldAlertIcon, ShieldCheckIcon, TriangleAlertIcon } from 'lucide-react'
import { Cell, DataTable, HeadCell, HeadRow, Row, RowNumberCell, RowNumberHeadCell, TableBody, TableHeader } from '@/components/data-table/DataTable'
import { ActionsCell, ActionsHeadCell } from '@/components/data-table/RowActions'
import { Button } from '@/components/ui/button'
import { EmptyState } from '@/components/ui/empty-state'
import { Skeleton } from '@/components/ui/skeleton'
import { StatCard, StatValue } from '@/components/ui/stat-card'
import { useAuth } from '@/features/auth/hooks/useAuth'
import { useNow } from '@/hooks/useNow'
import { useHostUpdates } from '../hooks/useUpdates'
import type { UpdatesPoint } from '../types/updates.types'
import { advisoryUrl, canViewUpdates, countText, formatDataAge, lastInstalledText, SOURCE_LABELS } from '../utils/updates'
import { EolText, RebootText } from './UpdatesBadges'

// La grafica usa Recharts: se descarga solo si hay evolucion que mostrar (fuera del bundle principal).
const PatchHistoryChart = lazy(() => import('./PatchHistoryChart').then((m) => ({ default: m.PatchHistoryChart })))

/** Icono de la tarjeta: ok en verde si no hay nada, aviso si hay algo; sin dato, el icono neutro. */
function tone(n: number | null | undefined): string | undefined {
  if (n === null || n === undefined) return undefined
  return n > 0 ? 'text-warn' : 'text-ok'
}

/** La grafica solo aporta si hubo algo pendiente o un reinicio en el periodo. */
function hadActivity(history: readonly UpdatesPoint[] | undefined): boolean {
  return !!history?.some((p) => (p.pending_total ?? 0) > 0 || (p.pending_security ?? 0) > 0 || p.reboot_required === true)
}

function Content({ hostId, onDenied }: { hostId: string; onDenied: () => void }) {
  const now = useNow(60_000)
  const { data, errorStatus, error } = useHostUpdates(hostId)
  useEffect(() => {
    if (errorStatus === 404) onDenied()
  }, [errorStatus, onDenied])
  if (errorStatus === 404 || (error && !data)) return null
  const u = data?.updates ?? null
  if (data && (data.status === 'no_data' || !u)) return <EmptyState Icon={ShieldCheckIcon} title="Sin datos de parches" />
  const pending = !data
  const advisories = u?.advisories ?? []
  const context = u
    ? [SOURCE_LABELS[u.source] ?? u.source, `último parche instalado ${lastInstalledText(u.last_installed)}`, `actualizado ${formatDataAge(u.data_age_s)}`]
    : []

  return (
    <div className="flex flex-col gap-3">
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard label="Pendientes" Icon={u?.pending_total === 0 ? CircleCheckIcon : Clock4Icon} iconClassName={tone(u?.pending_total)}>
          <StatValue pending={pending}>{u ? countText(u.pending_total) : null}</StatValue>
        </StatCard>
        <StatCard label="De seguridad" Icon={u?.pending_security === 0 ? ShieldCheckIcon : ShieldAlertIcon} iconClassName={u?.pending_security ? 'text-danger' : tone(u?.pending_security)}>
          <StatValue pending={pending}>{u ? countText(u.pending_security) : null}</StatValue>
        </StatCard>
        <StatCard label="Reinicio" Icon={RotateCwIcon} iconClassName={u?.reboot_required ? 'text-warn' : u ? 'text-ok' : undefined}>
          <StatValue pending={pending} size="md">
            {u ? (u.reboot_required ? <RebootText required={u.reboot_required} since={u.reboot_since} now={now} /> : 'No requiere') : null}
          </StatValue>
        </StatCard>
        <StatCard label="Fin de soporte" Icon={ShieldCheckIcon}>
          <StatValue pending={pending} size="md" detail={data?.eol ? data.eol.label : data ? 'versión no reconocida' : undefined}>
            {data ? <EolText eol={data.eol} /> : null}
          </StatValue>
        </StatCard>
      </div>

      {u && (
        <p className="flex flex-wrap items-center gap-x-2 text-xs text-text-muted">
          {context.join(' · ')}
          {u.stale && (
            <span className="inline-flex items-center gap-1">
              <TriangleAlertIcon className="size-3.5 shrink-0 text-warn" strokeWidth={1.5} aria-hidden />
              caché de paquetes antigua: los conteos pueden estar desactualizados
            </span>
          )}
        </p>
      )}

      {advisories.length > 0 && (
        <section aria-labelledby="host-advisories-title" className="flex flex-col gap-2">
          <h3 id="host-advisories-title" className="text-sm font-semibold">
            Avisos de seguridad pendientes
          </h3>
          <DataTable caption="Avisos de seguridad pendientes">
            <TableHeader>
              <HeadRow>
                <RowNumberHeadCell />
                <HeadCell>Aviso</HeadCell>
                <ActionsHeadCell />
              </HeadRow>
            </TableHeader>
            <TableBody>
              {advisories.map((a, i) => {
                const url = advisoryUrl(a)
                return (
                  <Row key={a}>
                    <RowNumberCell n={i + 1} />
                    <Cell className="font-semibold">{a}</Cell>
                    <ActionsCell>
                      {url && (
                        <Button asChild variant="ghost" size="icon-sm">
                          <a href={url} target="_blank" rel="noopener noreferrer" aria-label={`Abrir ${a} (sitio oficial)`} title="Abrir en el sitio oficial">
                            <ExternalLinkIcon strokeWidth={1.5} aria-hidden />
                          </a>
                        </Button>
                      )}
                    </ActionsCell>
                  </Row>
                )
              })}
            </TableBody>
          </DataTable>
        </section>
      )}

      {hadActivity(data?.history) && (
        <section aria-labelledby="host-patch-history-title" className="flex flex-col gap-2">
          <h3 id="host-patch-history-title" className="text-sm font-semibold">
            Evolución
          </h3>
          <Suspense fallback={<Skeleton className="h-48 w-full" />}>
            <PatchHistoryChart history={data?.history} />
          </Suspense>
        </section>
      )}
    </div>
  )
}

/** Pestaña "Parches" del host: solo admin o `can_updates`; un 404 la oculta. Una sola consulta. */
export function HostUpdatesSection({ hostId }: { hostId: string }) {
  const { user } = useAuth()
  const [denied, setDenied] = useState(false)
  const onDenied = useCallback(() => setDenied(true), [])
  if (!canViewUpdates(user) || denied) return null
  return <Content hostId={hostId} onDenied={onDenied} />
}
