import { useState } from 'react'
import { InlineError } from '@/components/ui/inline-error'
import { SegmentedControl } from '@/components/ui/segmented-control'
import { HostEventsSection } from '@/features/alerts/components/HostEventsSection'
import { HostChecksSection } from '@/features/checks/components/HostChecksSection'
import { HostInventorySection } from '@/features/inventory/components/HostInventorySection'
import { useAuth } from '@/features/auth/hooks/useAuth'
import { useHostDetail } from '../hooks/useHostDetail'
import { useHostMetrics } from '../hooks/useHostMetrics'
import type { MetricRange } from '../types/host.types'
import { RANGES } from '../utils/chart'
import { BackToHostsLink } from './BackToHostsLink'
import { EditHostDialog } from './EditHostDialog'
import { HostCharts } from './HostCharts'
import { HostDisks } from './HostDisks'
import { HostHeader } from './HostHeader'
import { HostLatestCards } from './HostLatestCards'
import { HostSystemInfo } from './HostSystemInfo'

const RANGE_OPTIONS = RANGES.map(({ value, label, ariaLabel }) => ({ value, label, ariaLabel }))

export function HostDetailView({ id }: { id: string }) {
  const { user } = useAuth()
  const [range, setRange] = useState<MetricRange>('1h')
  const [editing, setEditing] = useState(false)
  const detail = useHostDetail(id)
  const metrics = useHostMetrics(id, range)

  // 400 (id invalido) y 404 son definitivos: no hay nada mas que mostrar.
  const notFound = detail.errorStatus === 400 || detail.errorStatus === 404
  const data = detail.data

  return (
    <div className="flex flex-col gap-6">
      <BackToHostsLink />

      {notFound ? (
        <InlineError>Host no encontrado.</InlineError>
      ) : (
        <>
          {data ? (
            <HostHeader
              host={data.host}
              ip={data.info.ips[0] ?? null}
              canEdit={user?.role === 'admin'}
              onEdit={() => setEditing(true)}
            />
          ) : (
            detail.loading && <p className="text-sm text-text-muted">Cargando…</p>
          )}
          {detail.error && <InlineError>{detail.error}</InlineError>}

          {data && <HostSystemInfo host={data.host} info={data.info} />}

          <HostLatestCards
            latest={data?.latest ?? null}
            info={data?.info ?? null}
            status={data?.host.status ?? null}
            pending={detail.loading}
          />

          {data && <HostDisks disks={data.disks} />}

          <HostChecksSection hostId={id} />

          <HostEventsSection hostId={id} />

          <HostInventorySection hostId={id} />

          <div className="flex flex-col gap-4">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <h2 className="text-sm font-semibold">Métricas</h2>
              <SegmentedControl
                aria-label="Rango de tiempo"
                options={RANGE_OPTIONS}
                value={range}
                onChange={setRange}
              />
            </div>
            <HostCharts
              range={range}
              metrics={metrics.data}
              updatedAt={metrics.updatedAt}
              error={metrics.error}
            />
          </div>

          {data && (
            <EditHostDialog
              host={editing ? data.host : null}
              onClose={() => setEditing(false)}
              onSaved={() => {
                setEditing(false)
                void detail.refresh()
              }}
            />
          )}
        </>
      )}
    </div>
  )
}
