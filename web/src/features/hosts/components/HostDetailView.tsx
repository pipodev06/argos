import { useState } from 'react'
import { Link, useNavigate } from 'react-router'
import { Button } from '@/components/ui/button'
import { InlineError } from '@/components/ui/inline-error'
import { SegmentedControl } from '@/components/ui/segmented-control'
import { DeviceDetailView } from '@/features/devices/components/DeviceDetailView'
import { useAuth } from '@/features/auth/hooks/useAuth'
import { useHostDetail } from '../hooks/useHostDetail'
import { useHostMetrics } from '../hooks/useHostMetrics'
import type { MetricRange } from '../types/host.types'
import { RANGES, rangeView } from '../utils/chart'
import { BackLink } from '@/components/ui/back-link'
import { HostActionDialogs } from './HostActionDialogs'
import { HostTabs } from './HostTabs'
import { HostCharts } from './HostCharts'
import { HostDisks } from './HostDisks'
import { HostHeader } from './HostHeader'
import { HostLatestCards } from './HostLatestCards'
import { HostOpenAlerts } from './HostOpenAlerts'
import { HostSystemInfo } from './HostSystemInfo'
import type { HostActionId } from '../utils/hostActions'

const RANGE_OPTIONS = RANGES.map(({ value, label, ariaLabel }) => ({ value, label, ariaLabel }))

/** Un dispositivo sin agente tiene su propia vista (sin metricas ni inventario); el resto es un host con agente. */
export function HostDetailView({ id }: { id: string }) {
  const detail = useHostDetail(id)
  if (detail.data?.host.kind === 'device') return <DeviceDetailView id={id} />
  // Hasta conocer el tipo no se monta la vista de agente (pediria metricas que un dispositivo no tiene).
  if (detail.loading) {
    return (
      <div className="flex flex-col gap-3">
        <BackLink to="/hosts">Hosts</BackLink>
        <p className="text-sm text-text-muted">Cargando…</p>
      </div>
    )
  }
  return <AgentHostDetail id={id} detail={detail} />
}

function AgentHostDetail({ id, detail }: { id: string; detail: ReturnType<typeof useHostDetail> }) {
  const { user } = useAuth()
  const [range, setRange] = useState<MetricRange>('1h')
  const [action, setAction] = useState<HostActionId | null>(null)
  const navigate = useNavigate()
  const metrics = useHostMetrics(id, range)

  // 400 (id invalido) y 404 son definitivos: no hay nada mas que mostrar.
  const notFound = detail.errorStatus === 400 || detail.errorStatus === 404
  const data = detail.data

  return (
    <div className="flex flex-col gap-3">
      <BackLink to="/hosts">Hosts</BackLink>

      {notFound ? (
        <InlineError>Host no encontrado.</InlineError>
      ) : (
        <>
          {data ? (
            <HostHeader
              host={data.host}
              ip={data.info.ips[0] ?? null}
              onAction={user?.role === 'admin' ? setAction : undefined}
            />
          ) : (
            detail.loading && <p className="text-sm text-text-muted">Cargando…</p>
          )}
          {detail.error && <InlineError>{detail.error}</InlineError>}

          <HostTabs
            hostId={id}
            summary={
              <div className="flex flex-col gap-3">
                <HostLatestCards
                  latest={data?.latest ?? null}
                  info={data?.info ?? null}
                  status={data?.host.status ?? null}
                  pending={detail.loading}
                />
                <HostOpenAlerts hostId={id} />
                {data && <HostSystemInfo host={data.host} info={data.info} />}
                {data && <HostDisks disks={data.disks} physical={data.info.physical_disks} />}
              </div>
            }
            metrics={
              <div className="flex flex-col gap-3">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
                    <Button asChild variant="outline" size="sm">
                      <Link to={`/hosts/${encodeURIComponent(id)}/historial`}>Historial por fecha</Link>
                    </Button>
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
            }
          />

          {data && (
            <HostActionDialogs
              host={action ? data.host : null}
              action={action}
              onClose={() => setAction(null)}
              onDone={(done) => {
                setAction(null)
                if (done === 'delete') void navigate('/hosts')
                else void detail.refresh()
              }}
            />
          )}
        </>
      )}
    </div>
  )
}
