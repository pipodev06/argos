import { useCallback, useEffect, useState } from 'react'
import { IconArrowRight, IconCircleCheck, IconClockHour4, IconRefreshAlert, IconShieldCheck, IconShieldLock } from '@tabler/icons-react'
import { Link } from 'react-router'
import { Button } from '@/components/ui/button'
import { StatCard, StatValue } from '@/components/ui/stat-card'
import { useAuth } from '@/features/auth/hooks/useAuth'
import { useNow } from '@/hooks/useNow'
import { useHostUpdates } from '../hooks/useUpdates'
import { canViewUpdates, countText, formatDataAge } from '../utils/updates'
import { PatchHistoryChart } from './PatchHistoryChart'
import { EolText, RebootText } from './UpdatesBadges'

const MAX_ADVISORIES = 10

function Cards({ hostId, onDenied }: { hostId: string; onDenied: () => void }) {
  const now = useNow(60_000)
  const { data, errorStatus, error } = useHostUpdates(hostId)
  useEffect(() => {
    if (errorStatus === 404) onDenied()
  }, [errorStatus, onDenied])
  // Un error distinto de 404 no muestra nada: el detalle del host no debe llenarse de avisos secundarios.
  if (errorStatus === 404 || (error && !data)) return null
  const u = data?.updates ?? null
  if (data && (data.status === 'no_data' || !u)) {
    return <p className="text-sm text-text-muted">Sin datos aún: activa el módulo updates en el agente.</p>
  }
  const pending = !data
  const advisories = u?.advisories ?? []
  // Al día (nada pendiente ni reinicio): una sola línea en vez de tarjetas y gráfica, para no cargar el detalle.
  if (u && u.pending_total === 0 && u.reboot_required !== true) {
    return (
      <p className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm">
        <span className="inline-flex items-center gap-1.5">
          <IconCircleCheck className="size-4 shrink-0 text-ok" stroke={1.5} aria-hidden />
          Al día: sin parches pendientes ni reinicio
        </span>
        <span className="text-text-muted">·</span>
        <span className="inline-flex items-center gap-1.5">
          <span className="text-text-muted">Fin de vida:</span>
          <EolText eol={data?.eol ?? null} />
        </span>
        <span className="text-xs text-text-muted">
          Actualizado {formatDataAge(u.data_age_s)}
          {u.stale ? ' (caché de paquetes antigua)' : ''}
        </span>
      </p>
    )
  }
  return (
    <div className="flex flex-col gap-3">
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard label="Pendientes" Icon={IconClockHour4}>
          <StatValue pending={pending} detail={u ? `Actualizado ${formatDataAge(u.data_age_s)}` : undefined}>
            {u ? countText(u.pending_total) : null}
          </StatValue>
        </StatCard>
        <StatCard label="De seguridad" Icon={IconShieldLock}>
          <StatValue pending={pending}>{u ? countText(u.pending_security) : null}</StatValue>
        </StatCard>
        <StatCard label="Reinicio pendiente" Icon={IconRefreshAlert}>
          <StatValue pending={pending} size="md">
            {u ? <RebootText required={u.reboot_required} since={u.reboot_since} now={now} /> : null}
          </StatValue>
        </StatCard>
        <StatCard label="Fin de vida" Icon={IconShieldCheck}>
          <StatValue pending={pending} size="md" detail={data?.eol?.label}>
            {data ? <EolText eol={data.eol} /> : null}
          </StatValue>
        </StatCard>
      </div>
      {u && u.stale && (
        <p className="text-xs text-text-muted">La caché de paquetes del host es antigua: los conteos pueden estar desactualizados.</p>
      )}
      {advisories.length > 0 && (
        <p className="text-sm">
          <span className="font-semibold">Avisos de seguridad: </span>
          {advisories.slice(0, MAX_ADVISORIES).join(', ')}
          {advisories.length > MAX_ADVISORIES || u?.advisories_truncated ? ' y más' : ''}
        </p>
      )}
      <PatchHistoryChart history={data?.history} />
    </div>
  )
}

/** Parches en el detalle del host: solo admin o `can_updates`; un 404 lo oculta. Una sola consulta. */
export function HostUpdatesSection({ hostId }: { hostId: string }) {
  const { user } = useAuth()
  const [denied, setDenied] = useState(false)
  const onDenied = useCallback(() => setDenied(true), [])
  if (!canViewUpdates(user) || denied) return null
  return (
    <section className="flex flex-col gap-3" aria-labelledby="host-updates-title">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 id="host-updates-title" className="text-sm font-semibold">
          Parches
        </h2>
        <Button asChild variant="outline" size="sm">
          <Link to="/parches">
            Ver todos los hosts
            <IconArrowRight stroke={1.5} aria-hidden />
          </Link>
        </Button>
      </div>
      <Cards hostId={hostId} onDenied={onDenied} />
    </section>
  )
}
