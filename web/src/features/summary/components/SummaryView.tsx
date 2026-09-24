import {
  IconCircleCheck,
  IconCircleOff,
  IconCircleX,
  IconClock,
  IconPlugConnectedX,
  IconServer2,
} from '@tabler/icons-react'
import { Link } from 'react-router'
import { Button } from '@/components/ui/button'
import { EmptyState } from '@/components/ui/empty-state'
import { InlineError } from '@/components/ui/inline-error'
import { PageHeader } from '@/components/ui/page-header'
import { Skeleton } from '@/components/ui/skeleton'
import { StatusTile } from '@/components/ui/status-tile'
import { AlertsTile } from '@/features/alerts/components/AlertsTile'
import { useAuth } from '@/features/auth/hooks/useAuth'
import { useNow } from '@/hooks/useNow'
import { useSummary } from '../hooks/useSummary'
import type { Summary } from '../types/summary.types'
import { AttentionPanel } from './AttentionPanel'
import { GroupBreakdown } from './GroupBreakdown'
import { TopUsagePanel } from './TopUsagePanel'

interface SummaryViewProps {
  onAddHost: () => void
}

const TILE_LINK =
  'block rounded-md outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-bg'

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

  const c = data?.counts
  const tiles = [
    { label: 'Activos', value: c?.up, status: 'up', Icon: IconCircleCheck, icon: 'text-ok' },
    { label: 'Caídos', value: c?.down, status: 'down', Icon: IconCircleX, icon: 'text-danger' },
    { label: 'Sin conexión', value: c?.offline, status: 'offline', Icon: IconPlugConnectedX, icon: 'text-text-muted' },
    { label: 'Pendientes', value: c?.pending, status: 'pending', Icon: IconClock, icon: 'text-info' },
    { label: 'Deshabilitados', value: c?.disabled, status: 'disabled', Icon: IconCircleOff, icon: 'text-text-muted' },
  ] as const

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

        <div className="grid grid-cols-2 gap-4 md:grid-cols-3 xl:grid-cols-5">
          {tiles.map((t) =>
            c ? (
              <Link key={t.status} to={`/hosts?status=${t.status}`} className={TILE_LINK}>
                <StatusTile
                  label={t.label}
                  value={t.value ?? 0}
                  Icon={t.Icon}
                  iconClassName={t.icon}
                  alert={t.status === 'down' && (t.value ?? 0) > 0}
                />
              </Link>
            ) : (
              <Skeleton key={t.status} className="h-24" />
            ),
          )}
        </div>

        <AlertsTile />

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
