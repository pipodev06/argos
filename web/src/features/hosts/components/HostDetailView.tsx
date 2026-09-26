import { useState } from 'react'
import { Link, useNavigate } from 'react-router'
import { InlineError } from '@/components/ui/inline-error'
import { SegmentedControl } from '@/components/ui/segmented-control'
import { DeviceDetailView } from '@/features/devices/components/DeviceDetailView'
import { HostEventsSection } from '@/features/alerts/components/HostEventsSection'
import { HostChecksSection } from '@/features/checks/components/HostChecksSection'
import { HostInventorySummary } from '@/features/inventory/components/HostInventorySummary'
import { HostUpdatesSection } from '@/features/updates/components/HostUpdatesSection'
import { PurgeHostDialog } from '@/features/purge/components/PurgeHostDialog'
import { useAuth } from '@/features/auth/hooks/useAuth'
import { useHostDetail } from '../hooks/useHostDetail'
import { useHostMetrics } from '../hooks/useHostMetrics'
import type { MetricRange } from '../types/host.types'
import { RANGES, rangeView } from '../utils/chart'
import { BackToHostsLink } from './BackToHostsLink'
import { EditHostDialog } from './EditHostDialog'
import { HostCharts } from './HostCharts'
import { HostDisks } from './HostDisks'
import { HostHeader } from './HostHeader'
import { HostLatestCards } from './HostLatestCards'
import { HostSystemInfo } from './HostSystemInfo'

const RANGE_OPTIONS = RANGES.map(({ value, label, ariaLabel }) => ({ value, label, ariaLabel }))

/** Un dispositivo sin agente tiene su propia vista (sin metricas ni inventario); el resto es un host con agente. */
export function HostDetailView({ id }: { id: string }) {
  const detail = useHostDetail(id)
  if (detail.data?.host.kind === 'device') return <DeviceDetailView id={id} />
  // Hasta conocer el tipo no se monta la vista de agente (pediria metricas que un dispositivo no tiene).
  if (detail.loading) {
    return (
      <div className="flex flex-col gap-6">
        <BackToHostsLink />
        <p className="text-sm text-text-muted">Cargando…</p>
      </div>
    )
  }
  return <AgentHostDetail id={id} detail={detail} />
}

function AgentHostDetail({ id, detail }: { id: string; detail: ReturnType<typeof useHostDetail> }) {
  const { user } = useAuth()
  const [range, setRange] = useState<MetricRange>('1h')
  const [editing, setEditing] = useState(false)
  const [purging, setPurging] = useState(false)
  const navigate = useNavigate()
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
              onPurge={() => setPurging(true)}
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

          {data && <HostDisks disks={data.disks} physical={data.info.physical_disks} />}

          <HostChecksSection hostId={id} />

          <HostEventsSection hostId={id} />

          <HostInventorySummary hostId={id} />

          <HostUpdatesSection hostId={id} />

          <div className="flex flex-col gap-4">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
                <h2 className="text-sm font-semibold">Métricas</h2>
                <Link
                  to={`/hosts/${encodeURIComponent(id)}/historial`}
                  className="text-sm text-brand underline-offset-4 hover:underline"
                >
                  Historial por fecha
                </Link>
              </div>
              <SegmentedControl
                aria-label="Rango de tiempo"
                options={RANGE_OPTIONS}
                value={range}
                onChange={setRange}
              />
            </div>
            <HostCharts
              view={rangeView(range, metrics.updatedAt ?? 0)}
              metrics={metrics.data}
              error={metrics.error}
            />
          </div>

          {data && (
            <PurgeHostDialog
              host={purging ? data.host : null}
              onClose={() => setPurging(false)}
              onDone={() => void navigate('/hosts')}
            />
          )}

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
