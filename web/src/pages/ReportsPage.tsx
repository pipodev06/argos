import { IconReportAnalytics } from '@tabler/icons-react'
import { NoResultsState, TableError } from '@/components/data-table/TableStates'
import { FilterBar } from '@/components/filters/FilterBar'
import { EmptyState } from '@/components/ui/empty-state'
import { PageHeader } from '@/components/ui/page-header'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { ReportAvailabilityTable } from '@/features/reports/components/ReportAvailabilityTable'
import { ReportServiceChecksTable } from '@/features/reports/components/ReportServiceChecksTable'
import { ReportSummaryTiles } from '@/features/reports/components/ReportSummaryTiles'
import { useAvailabilityReport, useServiceChecksReport } from '@/features/reports/hooks/useReports'
import { REPORT_FIELDS } from '@/features/reports/utils/reportQuery'
import type { ReportRange, ReportView } from '@/features/reports/types/report.types'
import { useUrlFilters } from '@/hooks/useUrlFilters'

export function ReportsPage() {
  const { filters, setFilters, clear, hasActive } = useUrlFilters(REPORT_FIELDS)

  // Default values
  const reportType = filters.type[0] || 'availability'
  const range = (filters.range[0] || '30d') as ReportRange
  const by = (filters.by[0] || 'host') as ReportView
  const groupId = undefined // TODO: add group filter

  const { data: availabilityReport, error: availError, loading: availLoading, refresh: refreshAvail } = useAvailabilityReport(range, by, groupId)
  const { data: checksReport, error: checksError, loading: checksLoading, refresh: refreshChecks } = useServiceChecksReport(range, groupId)

  const showAvailability = reportType === 'availability'
  const data = showAvailability ? availabilityReport : checksReport
  const error = showAvailability ? availError : checksError
  const loading = showAvailability ? availLoading : checksLoading
  const refresh = showAvailability ? refreshAvail : refreshChecks

  const hasData = data !== undefined

  return (
    <>
      <PageHeader title="Informes" />
      <div className="flex flex-col gap-3" aria-busy={loading}>
        <FilterBar
          fields={REPORT_FIELDS}
          values={filters}
          onChange={setFilters}
          onClear={clear}
          itemLabel="informes"
          total={null}
          overall={null}
        />

        {error && <TableError message={error} onRetry={() => void refresh()} />}

        {loading && !hasData && <Skeleton className="h-40 w-full" />}

        {hasData && data && (
          <>
            <ReportSummaryTiles fleet={data.fleet} />

            <Tabs defaultValue="availability" value={reportType} onValueChange={(v) => setFilters({ ...filters, type: [v] })}>
              <TabsList>
                <TabsTrigger value="availability">Disponibilidad</TabsTrigger>
                <TabsTrigger value="service-checks">Chequeos</TabsTrigger>
              </TabsList>

              <TabsContent value="availability">
                {availabilityReport ? (
                  availabilityReport.items.length === 0 && hasActive ? (
                    <NoResultsState Icon={IconReportAnalytics} title="Ningún resultado coincide con los filtros." onClear={clear} />
                  ) : (
                    <ReportAvailabilityTable items={availabilityReport.items} isGroupView={availabilityReport.by === 'group'} />
                  )
                ) : (
                  <EmptyState Icon={IconReportAnalytics} title="Sin datos de disponibilidad" />
                )}
              </TabsContent>

              <TabsContent value="service-checks">
                {checksReport ? (
                  checksReport.items.length === 0 && hasActive ? (
                    <NoResultsState Icon={IconReportAnalytics} title="Ningún resultado coincide con los filtros." onClear={clear} />
                  ) : (
                    <ReportServiceChecksTable items={checksReport.items} />
                  )
                ) : (
                  <EmptyState Icon={IconReportAnalytics} title="Sin datos de chequeos" />
                )}
              </TabsContent>
            </Tabs>
          </>
        )}
      </div>
    </>
  )
}
