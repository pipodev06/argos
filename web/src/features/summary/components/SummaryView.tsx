import { IconServer2 } from '@tabler/icons-react'
import { Button } from '@/components/ui/button'
import { EmptyState } from '@/components/ui/empty-state'
import { InlineError } from '@/components/ui/inline-error'
import { PageHeader } from '@/components/ui/page-header'
import { Skeleton } from '@/components/ui/skeleton'
import { AlertsBlock } from '@/features/alerts/components/AlertsBlock'
import { useAuth } from '@/features/auth/hooks/useAuth'
import { useNow } from '@/hooks/useNow'
import { useSummary } from '../hooks/useSummary'
import type { Summary } from '../types/summary.types'
import { AttentionPanel } from './AttentionPanel'
import { GroupBreakdown } from './GroupBreakdown'
import { StatusBlock } from './StatusBlock'
import { TopUsagePanel } from './TopUsagePanel'

interface SummaryViewProps {
  onAddHost: () => void
}

function ago(from: number, now: number): string {
  const seconds = Math.max(0, Math.floor((now - from) / 1000))
  if (seconds < 60) return `${seconds} s`
  return `${Math.floor(seconds / 60)} min`
}

function total(counts: Summary['counts']): number {
  return counts.up + counts.down + counts.offline + counts.pending + counts.disabled
}

export function SummaryView({ onAddHost }: SummaryViewProps) {
  const { user } = useAuth()
  const { data, error, loading, updatedAt, refresh } = useSummary()
  const now = useNow(5_000)
  const isAdmin = user?.role === 'admin'

  const title = (
    <PageHeader
      title="Resumen"
      actions={
        updatedAt !== null && (
          <span className="text-xs text-text-muted tabular-nums">Actualizado hace {ago(updatedAt, now)}</span>
        )
      }
    />
  )

  if (data && total(data.counts) === 0) {
    return (
      <>
        {title}
        <EmptyState
          Icon={IconServer2}
          title="Aún no hay hosts"
          description={isAdmin ? 'Agrega el primero para empezar a monitorear.' : 'Un administrador debe enrolar el primero.'}
          action={isAdmin && <Button size="sm" onClick={onAddHost}>Agregar host</Button>}
        />
      </>
    )
  }

  const showDevices = data ? total(data.devices) > 0 : false

  return (
    <>
      {title}
      <div className="flex flex-col gap-6" aria-busy={loading}>
        {error && (
          <div className="flex flex-wrap items-center gap-3">
            <InlineError>
              {error}
              {updatedAt !== null && ` Datos de hace ${ago(updatedAt, now)}.`}
            </InlineError>
            <Button variant="outline" size="sm" onClick={() => void refresh()}>
              Reintentar
            </Button>
          </div>
        )}

        {/* Tres bloques de 3 filas (hosts 2x3, dispositivos 2x3, alertas 1x3) alineados; sin dispositivos, dos. */}
        <div className={'grid gap-6 ' + (showDevices ? 'lg:grid-cols-[2fr_2fr_1fr]' : 'lg:grid-cols-[2fr_1fr]')}>
          <StatusBlock id="summary-hosts" title="Hosts" counts={data?.counts} to="/hosts" />
          {showDevices && <StatusBlock id="summary-devices" title="Dispositivos" counts={data?.devices} to="/dispositivos" />}
          <AlertsBlock />
        </div>

        {data ? (
          <div className="grid gap-6 lg:grid-cols-2">
            <AttentionPanel items={data.recently_down} now={now} />
            <GroupBreakdown groups={data.by_group} />
            <div className="lg:col-span-2">
              <TopUsagePanel summary={data} />
            </div>
          </div>
        ) : (
          !error && <Skeleton className="h-48 w-full" />
        )}
      </div>
    </>
  )
}
