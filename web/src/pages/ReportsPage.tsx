import { CalendarIcon, DownloadIcon, FileChartColumnIcon } from 'lucide-react'
import { TableError } from '@/components/data-table/TableStates'
import { InlineError } from '@/components/ui/inline-error'
import { Button } from '@/components/ui/button'
import { EmptyState } from '@/components/ui/empty-state'
import { DateTimePicker } from '@/components/ui/date-time-picker'
import { PageHeader } from '@/components/ui/page-header'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { SegmentedControl } from '@/components/ui/segmented-control'
import { Skeleton } from '@/components/ui/skeleton'
import { useGroups } from '@/features/groups/hooks/useGroups'
import { parseLocalInput, toLocalInput, toUtcIso } from '@/features/hosts/utils/history'
import { ReportAvailabilityTable } from '@/features/reports/components/ReportAvailabilityTable'
import { ReportServiceChecksTable } from '@/features/reports/components/ReportServiceChecksTable'
import { ReportSummaryTiles } from '@/features/reports/components/ReportSummaryTiles'
import { useAvailabilityReport, useServiceChecksReport } from '@/features/reports/hooks/useReports'
import { reportCsvUrl } from '@/features/reports/services/reports.service'
import type { ReportRange, ReportView } from '@/features/reports/types/report.types'
import { RANGE_OPTIONS, REPORT_FIELDS, TYPE_OPTIONS, VIEW_OPTIONS } from '@/features/reports/utils/reportQuery'
import { useNow } from '@/hooks/useNow'
import { useUrlFilters } from '@/hooks/useUrlFilters'

const ALL_GROUPS = 'all'
/** Tope del rango libre; igual que el servidor (maxReportRangeDays). */
const MAX_RANGE_DAYS = 91

/** Rango de los atajos relativos ("Hoy" = desde las 00:00 locales; "24 h"); null en los demas. */
function relativeRange(range: string, now: number): { from: string; to: string } | null {
  if (range !== 'today' && range !== '24h') return null
  const end = new Date(now)
  end.setSeconds(0, 0)
  const start = range === 'today' ? new Date(end.getFullYear(), end.getMonth(), end.getDate()) : new Date(end.getTime() - 24 * 3600 * 1000)
  return { from: start.toISOString(), to: end.toISOString() }
}

/** Valida el rango libre antes de pedirlo (el servidor responderia 400 con un texto tecnico). */
function customRangeError(from: string, to: string): string | null {
  const ms = new Date(to).getTime() - new Date(from).getTime()
  if (ms <= 0) return 'Desde debe ser anterior a Hasta.'
  if (ms > MAX_RANGE_DAYS * 24 * 3600 * 1000) return `El rango no puede pasar de ${MAX_RANGE_DAYS} días.`
  return null
}

interface RangeProps {
  range: ReportRange
  by?: ReportView
  groupId?: string
  from?: string
  to?: string
}

function AvailabilityView({ range, by = 'host', groupId, from, to }: RangeProps) {
  const { data, error, loading, refresh } = useAvailabilityReport(range, by, groupId, from, to)
  if (error) return <TableError message={error} onRetry={() => void refresh()} />
  if (!data) return loading ? <Skeleton className="h-40 w-full" /> : null
  return (
    <>
      <ReportSummaryTiles fleet={data.fleet} />
      {data.items.length === 0 ? (
        <EmptyState Icon={FileChartColumnIcon} title="Sin hosts" />
      ) : (
        <ReportAvailabilityTable items={data.items} isGroupView={data.by === 'group'} />
      )}
    </>
  )
}

function ServiceChecksView({ range, groupId, from, to }: RangeProps) {
  const { data, error, loading, refresh } = useServiceChecksReport(range, groupId, from, to)
  if (error) return <TableError message={error} onRetry={() => void refresh()} />
  if (!data) return loading ? <Skeleton className="h-40 w-full" /> : null
  return data.items.length === 0 ? (
    <EmptyState Icon={FileChartColumnIcon} title="Sin chequeos" />
  ) : (
    <ReportServiceChecksTable items={data.items} />
  )
}

export function ReportsPage() {
  const { filters, setFilters } = useUrlFilters(REPORT_FIELDS)
  const { type, range, by } = filters as { type: string; range: ReportRange; by: ReportView }
  const groupId = filters.group || undefined
  const { groups } = useGroups()
  const csvKind = type === 'availability' ? 'availability' : 'service-checks'

  // Rango libre (B12): from/to en la URL como RFC3339; los inputs muestran/editan la hora local.
  const isCustom = range === 'custom'
  // "Hoy" y "24 h" se piden como rango libre calculado aqui (se renueva cada minuto).
  const now = useNow(60_000)
  const relative = relativeRange(range, now)
  const from = relative?.from ?? (isCustom && filters.from ? filters.from : undefined)
  const to = relative?.to ?? (isCustom && filters.to ? filters.to : undefined)
  const customIncomplete = isCustom && !(from && to)
  // Tope de los calendarios: hoy (no hay datos futuros).
  const today = new Date(now)
  const rangeError = isCustom && from && to ? customRangeError(from, to) : null
  const blocked = customIncomplete || rangeError !== null

  function exportButton(kind: Parameters<typeof reportCsvUrl>[0], label: string) {
    const content = (
      <>
        <DownloadIcon aria-hidden strokeWidth={1.5} />
        {label}
      </>
    )
    return blocked ? (
      <Button variant="outline" size="sm" disabled>
        {content}
      </Button>
    ) : (
      <Button asChild variant="outline" size="sm">
        <a href={reportCsvUrl(kind, range, groupId, from, to)} download>
          {content}
        </a>
      </Button>
    )
  }

  return (
    <>
      <PageHeader
        title="Informes"
        actions={
          <>
            {exportButton(csvKind, 'Exportar CSV')}
            {exportButton('alerts', 'Exportar alertas')}
          </>
        }
      />
      <div className="flex flex-col gap-3">
        <div className="flex flex-wrap items-center gap-2">
          <SegmentedControl
            aria-label="Informe"
            options={TYPE_OPTIONS}
            value={type as (typeof TYPE_OPTIONS)[number]['value']}
            onChange={(v) => setFilters({ type: v })}
          />
          <SegmentedControl aria-label="Rango de tiempo" options={RANGE_OPTIONS} value={range} onChange={(v) => setFilters({ range: v })} />
          {type === 'availability' && (
            <SegmentedControl aria-label="Vista" options={VIEW_OPTIONS} value={by} onChange={(v) => setFilters({ by: v })} />
          )}
          <Select value={groupId ?? ALL_GROUPS} onValueChange={(v) => setFilters({ group: v === ALL_GROUPS ? '' : v })}>
            <SelectTrigger aria-label="Grupo" className="w-40">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL_GROUPS}>Todos los grupos</SelectItem>
              {groups?.items.map((g) => (
                <SelectItem key={g.id} value={g.id}>
                  {g.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {isCustom && (
            <>
              <DateTimePicker
                aria-label="Desde"
                placeholder="Desde"
                className="w-48"
                maxDate={today}
                value={filters.from ? toLocalInput(new Date(filters.from).getTime()) : ''}
                onChange={(v) => setFilters({ from: v ? toUtcIso(parseLocalInput(v)) : '' })}
              />
              <DateTimePicker
                aria-label="Hasta"
                placeholder="Hasta"
                className="w-48"
                maxDate={today}
                value={filters.to ? toLocalInput(new Date(filters.to).getTime()) : ''}
                onChange={(v) => setFilters({ to: v ? toUtcIso(parseLocalInput(v)) : '' })}
              />
            </>
          )}
        </div>
        {customIncomplete ? (
          <EmptyState Icon={CalendarIcon} title="Elige Desde y Hasta" />
        ) : rangeError ? (
          <InlineError>{rangeError}</InlineError>
        ) : type === 'availability' ? (
          <AvailabilityView range={range} by={by} groupId={groupId} from={from} to={to} />
        ) : (
          <ServiceChecksView range={range} groupId={groupId} from={from} to={to} />
        )}
      </div>
    </>
  )
}
