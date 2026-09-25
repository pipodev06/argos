import { IconAlertTriangle, IconCircleCheck, IconCircleX, IconHistory } from '@tabler/icons-react'
import { StatCard, StatValue } from '@/components/ui/stat-card'
import { AVAILABILITY_TARGET_PCT, formatAvailabilityPercent, formatCoveragePct, formatSeconds } from '../utils/format'
import type { ReportFleet } from '../types/report.types'

/** Tiles de resumen de flota (patron StatCard: icono de estado + etiqueta xs muted + cifra 2xl tabular). */
export function ReportSummaryTiles({ fleet }: { fleet: ReportFleet }) {
  const meets = fleet.availability_pct !== null && fleet.availability_pct >= AVAILABILITY_TARGET_PCT
  return (
    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
      <StatCard label="Disponibilidad" Icon={IconCircleCheck} iconClassName={meets ? 'text-ok' : undefined}>
        <StatValue>{fleet.availability_pct === null ? null : formatAvailabilityPercent(fleet.availability_pct)}</StatValue>
      </StatCard>
      <StatCard label="Tiempo caído" Icon={IconCircleX} iconClassName="text-danger" alert={fleet.down_s > 0}>
        <StatValue>{formatSeconds(fleet.down_s)}</StatValue>
      </StatCard>
      <StatCard label="Incidentes" Icon={IconAlertTriangle} iconClassName="text-warn">
        <StatValue>{fleet.incidents.toLocaleString('es-PE')}</StatValue>
      </StatCard>
      <StatCard label="Cobertura" Icon={IconHistory}>
        <StatValue>{formatCoveragePct(fleet.coverage_pct)}</StatValue>
      </StatCard>
    </div>
  )
}
