import { IconDownload, IconReportAnalytics } from '@tabler/icons-react'
import { TableError } from '@/components/data-table/TableStates'
import { Button } from '@/components/ui/button'
import { EmptyState } from '@/components/ui/empty-state'
import { FormField } from '@/components/ui/form-field'
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
        <EmptyState Icon={IconReportAnalytics} title="Sin hosts en el informe" />
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
    <EmptyState Icon={IconReportAnalytics} title="Sin chequeos en el informe" />
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

  return (
    <>
      <PageHeader title="Informes" />
      <div className="flex flex-col gap-3">
        <div className="flex flex-wrap items-center gap-3">
          <SegmentedControl
            aria-label="Informe"
            options={TYPE_OPTIONS}
            value={type as (typeof TYPE_OPTIONS)[number]['value']}
            onChange={(v) => setFilters({ type: v })}
          />
          <SegmentedControl aria-label="Rango de tiempo" options={RANGE_OPTIONS} value={range} onChange={(v) => setFilters({ range: v })} />
          {isCustom && (
            <>
              <FormField
                id="report-from"
                label="Desde"
                type="datetime-local"
                className="w-56"
                value={filters.from ? toLocalInput(new Date(filters.from).getTime()) : ''}
                onChange={(e) => setFilters({ from: e.target.value ? toUtcIso(parseLocalInput(e.target.value)) : '' })}
              />
              <FormField
                id="report-to"
                label="Hasta"
                type="datetime-local"
                className="w-56"
                value={filters.to ? toLocalInput(new Date(filters.to).getTime()) : ''}
                onChange={(e) => setFilters({ to: e.target.value ? toUtcIso(parseLocalInput(e.target.value)) : '' })}
              />
            </>
          )}
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
          <div className="ml-auto flex flex-wrap items-center gap-2">
            {customIncomplete ? (
              <>
                <Button variant="outline" size="sm" disabled>
                  <IconDownload aria-hidden stroke={1.5} />
                  Exportar CSV
                </Button>
                <Button variant="outline" size="sm" disabled>
                  <IconDownload aria-hidden stroke={1.5} />
                  Exportar alertas
                </Button>
              </>
            ) : (
              <>
                <Button asChild variant="outline" size="sm">
                  <a href={reportCsvUrl(csvKind, range, groupId, from, to)} download>
                    <IconDownload aria-hidden stroke={1.5} />
                    Exportar CSV
                  </a>
                </Button>
                <Button asChild variant="outline" size="sm">
                  <a href={reportCsvUrl('alerts', range, groupId, from, to)} download>
                    <IconDownload aria-hidden stroke={1.5} />
                    Exportar alertas
                  </a>
                </Button>
              </>
            )}
          </div>
        </div>
        {customIncomplete ? (
          <p className="text-sm text-text-muted">Elige "Desde" y "Hasta" para ver el informe.</p>
        ) : type === 'availability' ? (
          <AvailabilityView range={range} by={by} groupId={groupId} from={from} to={to} />
        ) : (
          <ServiceChecksView range={range} groupId={groupId} from={from} to={to} />
        )}
      </div>
    </>
  )
}
