import { CalendarIcon, DownloadIcon, FileChartColumnIcon } from 'lucide-react'
import { TableError } from '@/components/data-table/TableStates'
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
import { useUrlFilters } from '@/hooks/useUrlFilters'

const ALL_GROUPS = 'all'

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
  const from = isCustom && filters.from ? filters.from : undefined
  const to = isCustom && filters.to ? filters.to : undefined
  const customIncomplete = isCustom && !(from && to)

  function exportButton(kind: Parameters<typeof reportCsvUrl>[0], label: string) {
    const content = (
      <>
        <DownloadIcon aria-hidden strokeWidth={1.5} />
        {label}
      </>
    )
    return customIncomplete ? (
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
      <div className="flex flex-col gap-4">
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
        </div>
        {isCustom && (
          <div className="flex flex-wrap items-center gap-2">
            <DateTimePicker
              aria-label="Desde"
              placeholder="Desde"
              className="w-56"
              value={filters.from ? toLocalInput(new Date(filters.from).getTime()) : ''}
              onChange={(v) => setFilters({ from: v ? toUtcIso(parseLocalInput(v)) : '' })}
            />
            <DateTimePicker
              aria-label="Hasta"
              placeholder="Hasta"
              className="w-56"
              value={filters.to ? toLocalInput(new Date(filters.to).getTime()) : ''}
              onChange={(v) => setFilters({ to: v ? toUtcIso(parseLocalInput(v)) : '' })}
            />
          </div>
        )}
        {customIncomplete ? (
          <EmptyState Icon={CalendarIcon} title="Elige Desde y Hasta" />
        ) : type === 'availability' ? (
          <AvailabilityView range={range} by={by} groupId={groupId} from={from} to={to} />
        ) : (
          <ServiceChecksView range={range} groupId={groupId} from={from} to={to} />
        )}
      </div>
    </>
  )
}
