import { useCallback } from 'react'
import { ViewAction } from '@/components/data-table/RowActions'
import { InlineError } from '@/components/ui/inline-error'
import { Skeleton } from '@/components/ui/skeleton'
import { DeviceStatusBadge } from '@/features/devices/components/DeviceBadges'
import { listDevices } from '@/features/devices/services/devices.service'
import { isClientError, usePolledFetch } from '@/hooks/usePolledFetch'
import { formatLastSeen } from '../utils/format'

const OPTIONS = { intervalMs: 30_000, stopOn: isClientError }

/** Dispositivos que este host sondea como colector (ping/SNMP): el chequeo que hace "hacia afuera". */
export function CollectorDevices({ hostId }: { hostId: string }) {
  const fetcher = useCallback(
    (signal: AbortSignal) => listDevices(new URLSearchParams({ collector: hostId, limit: '100' }), signal),
    [hostId],
  )
  const { data, error } = usePolledFetch(fetcher, OPTIONS)
  if (error && !data) return <InlineError>{error}</InlineError>
  if (!data) return <Skeleton className="h-12 w-full" />
  if (data.items.length === 0) return null
  return (
    <section className="flex flex-col gap-2" aria-labelledby="collector-devices-title">
      <h3 id="collector-devices-title" className="text-sm font-semibold">
        Dispositivos que sondea
      </h3>
      <ul className="flex flex-col overflow-hidden rounded-md border border-border bg-surface">
        {data.items.map((d) => (
          <li key={d.id} className="flex min-h-8 items-center gap-3 border-b border-border px-2 py-1 text-sm last:border-b-0">
            <span className="min-w-0 truncate font-semibold">{d.name}</span>
            <span className="tabular-nums text-text-muted">{d.address ?? '—'}</span>
            <DeviceStatusBadge device={d} />
            <span className="ml-auto text-text-muted tabular-nums">{formatLastSeen(d.last_seen_at)}</span>
            <ViewAction to={`/dispositivos/${d.id}`} label={d.name} />
          </li>
        ))}
      </ul>
    </section>
  )
}
