import { IconReportAnalytics } from '@tabler/icons-react'
import { TableError } from '@/components/data-table/TableStates'
import { EmptyState } from '@/components/ui/empty-state'
import { PageHeader } from '@/components/ui/page-header'
import { SegmentedControl } from '@/components/ui/segmented-control'
import { Skeleton } from '@/components/ui/skeleton'
import { ReportAvailabilityTable } from '@/features/reports/components/ReportAvailabilityTable'
import { ReportServiceChecksTable } from '@/features/reports/components/ReportServiceChecksTable'
import { ReportSummaryTiles } from '@/features/reports/components/ReportSummaryTiles'
import { useAvailabilityReport, useServiceChecksReport } from '@/features/reports/hooks/useReports'
import type { ReportRange, ReportView } from '@/features/reports/types/report.types'
import { RANGE_OPTIONS, REPORT_FIELDS, TYPE_OPTIONS, VIEW_OPTIONS } from '@/features/reports/utils/reportQuery'
import { useUrlFilters } from '@/hooks/useUrlFilters'

function AvailabilityView({ range, by }: { range: ReportRange; by: ReportView }) {
  const { data, error, loading, refresh } = useAvailabilityReport(range, by)
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

function ServiceChecksView({ range }: { range: ReportRange }) {
  const { data, error, loading, refresh } = useServiceChecksReport(range)
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
          {type === 'availability' && (
            <SegmentedControl aria-label="Vista" options={VIEW_OPTIONS} value={by} onChange={(v) => setFilters({ by: v })} />
          )}
        </div>
        {type === 'availability' ? <AvailabilityView range={range} by={by} /> : <ServiceChecksView range={range} />}
      </div>
    </>
  )
}
