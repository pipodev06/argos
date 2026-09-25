import type { ComponentType } from 'react'
import { IconAlertTriangle, IconTrendingDown } from '@tabler/icons-react'
import { formatAvailabilityPercent, formatSeconds } from '../utils/format'
import type { ReportFleet } from '../types/report.types'

interface ReportSummaryTilesProps {
  fleet: ReportFleet
}

type TileIcon = ComponentType<{ className?: string; stroke?: number; 'aria-hidden'?: boolean }>

function SummaryTile({ icon: Icon, label, value, isDowntime }: { icon: TileIcon | null; label: string; value: string; isDowntime?: boolean }) {
  return (
    <div className="flex flex-col gap-2 rounded-md border border-border bg-surface p-4">
      <div className="flex items-center gap-2">
        {Icon && <Icon className="size-4 text-text-muted" stroke={1.5} aria-hidden />}
        <span className="text-xs text-text-muted">{label}</span>
      </div>
      <div className={`text-2xl font-tabular-nums font-semibold ${isDowntime ? 'text-danger' : 'text-text'}`}>{value}</div>
    </div>
  )
}

export function ReportSummaryTiles({ fleet }: ReportSummaryTilesProps) {
  return (
    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
      <SummaryTile label="Disponibilidad" icon={null} value={formatAvailabilityPercent(fleet.availability_pct)} />
      <SummaryTile label="Tiempo caído" icon={IconTrendingDown} value={formatSeconds(fleet.down_s)} isDowntime />
      <SummaryTile label="Incidentes" icon={IconAlertTriangle} value={String(fleet.incidents)} />
      <SummaryTile label="Cobertura" icon={null} value={`${Math.round(fleet.coverage_pct)}%`} />
    </div>
  )
}
