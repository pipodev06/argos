import { useCallback } from 'react'
import { ViewAction } from '@/components/data-table/RowActions'
import { AlertStateBadge, SeverityBadge } from '@/features/alerts/components/AlertBadges'
import { listAlerts } from '@/features/alerts/services/alerts.service'
import { isClientError, usePolledFetch } from '@/hooks/usePolledFetch'
import { useNow } from '@/hooks/useNow'
import { formatLastSeen } from '../utils/format'

const OPTIONS = { intervalMs: 30_000, stopOn: isClientError }

/** Alertas abiertas de este host (pestaña Resumen); no ocupa lugar si no hay ninguna. */
export function HostOpenAlerts({ hostId }: { hostId: string }) {
  const now = useNow(30_000)
  const fetcher = useCallback(
    (signal: AbortSignal) => listAlerts(new URLSearchParams({ host_id: hostId, state: 'open', limit: '10' }), signal),
    [hostId],
  )
  const { data } = usePolledFetch(fetcher, OPTIONS)
  if (!data || data.items.length === 0) return null
  return (
    <section aria-labelledby="host-open-alerts-title" className="flex flex-col gap-2">
      <h2 id="host-open-alerts-title" className="text-sm font-semibold">
        Alertas abiertas
      </h2>
      <ul className="flex flex-col overflow-hidden rounded-md border border-border bg-surface">
        {data.items.map((a) => (
          <li key={a.id} className="flex min-h-8 items-center gap-3 border-b border-border px-2 py-1 text-sm last:border-b-0">
            <SeverityBadge severity={a.severity} />
            <span className="min-w-0 truncate font-semibold">{a.rule_name}</span>
            <AlertStateBadge alert={a} />
            <span className="ml-auto text-text-muted tabular-nums">{formatLastSeen(a.opened_at, now)}</span>
            <ViewAction to={`/alertas/${a.id}`} label={`alerta ${a.rule_name}`} />
          </li>
        ))}
      </ul>
    </section>
  )
}
