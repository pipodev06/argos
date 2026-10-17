import { useMemo, useState } from 'react'
import { IconDownload, IconInfoCircle } from '@tabler/icons-react'
import { Button } from '@/components/ui/button'
import { DateTimeField } from '@/components/ui/date-time-picker'
import { InlineError } from '@/components/ui/inline-error'
import { ApiError } from '@/lib/api'
import { useHostDetail } from '../hooks/useHostDetail'
import { useHostHistory } from '../hooks/useHostHistory'
import { downloadHostHistoryCsv } from '../services/hosts.service'
import {
  DETAIL_RETENTION_DAYS,
  SHORTCUTS,
  dayRange,
  historyView,
  parseLocalInput,
  shortcutRange,
  toLocalDate,
  toLocalInput,
  validateRange,
  type DateRange,
  type HistorySeries,
} from '../utils/history'
import { formatFullDate } from '../utils/format'
import { BackToHostsLink } from './BackToHostsLink'
import { HostCharts } from './HostCharts'

function exportErrorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.status === 429) {
      return err.retryAfter
        ? `Demasiadas exportaciones seguidas. Reintenta en ${err.retryAfter} s.`
        : 'Demasiadas exportaciones seguidas. Reintenta en unos segundos.'
    }
    return err.message
  }
  return 'No se pudo exportar el CSV.'
}

/** Historial por fecha: gráficas de un rango elegido (no relativo a "ahora") y exportación CSV. */
export function HostHistoryView({ id }: { id: string }) {
  const detail = useHostDetail(id)
  const [initial] = useState(() => shortcutRange('yesterday', Date.now()))
  const [applied, setApplied] = useState<DateRange>(initial)
  const [from, setFrom] = useState(() => toLocalInput(initial.fromMs))
  const [to, setTo] = useState(() => toLocalInput(initial.toMs))
  const [day, setDay] = useState(() => toLocalDate(initial.fromMs))
  const [formError, setFormError] = useState<string | null>(null)
  const [exporting, setExporting] = useState<HistorySeries | null>(null)
  const [exportError, setExportError] = useState<string | null>(null)
  const history = useHostHistory(id, applied)
  const notFound = detail.errorStatus === 400 || detail.errorStatus === 404 || history.errorStatus === 404
  const hostname = detail.data?.host.hostname ?? history.data?.host.hostname ?? ''

  function apply(range: DateRange) {
    const err = validateRange(range, Date.now(), history.data?.detail_retention_days)
    setFormError(err)
    setExportError(null)
    if (!err) setApplied(range)
  }

  function applyPreset(range: DateRange) {
    setFrom(toLocalInput(range.fromMs))
    setTo(toLocalInput(range.toMs))
    setDay(toLocalDate(range.fromMs))
    apply(range)
  }

  function onDayChange(value: string) {
    setDay(value)
    const range = dayRange(value)
    if (range) applyPreset(range)
  }

  async function exportCsv(series: HistorySeries) {
    setExporting(series)
    setExportError(null)
    try {
      await downloadHostHistoryCsv(id, hostname, applied, series)
    } catch (err) {
      setExportError(exportErrorMessage(err))
    } finally {
      setExporting(null)
    }
  }

  const data = history.data
  const view = useMemo(
    () => historyView(applied.fromMs, applied.toMs, data?.step_seconds ?? 30),
    [applied, data?.step_seconds],
  )
  const availableFrom = data?.available_from ? new Date(data.available_from).getTime() : null
  const startsBeforeData = data && availableFrom !== null && availableFrom > applied.fromMs + data.step_seconds * 1000

  return (
    <div className="flex flex-col gap-6">
      <BackToHostsLink to={`/hosts/${encodeURIComponent(id)}`}>Volver al host</BackToHostsLink>
      {notFound ? (
        <InlineError>Host no encontrado.</InlineError>
      ) : (
        <>
          <h1 className="min-w-0 truncate text-lg font-semibold">
            Historial por fecha{hostname ? ` · ${hostname}` : ''}
          </h1>

          <form
            className="flex flex-col gap-3 rounded-md border border-border bg-surface p-4"
            onSubmit={(e) => {
              e.preventDefault()
              apply({ fromMs: parseLocalInput(from), toMs: parseLocalInput(to) })
            }}
            noValidate
          >
            <div className="flex flex-wrap items-end gap-2">
              <div className="w-60">
                <DateTimeField id="history-from" label="Desde" value={from} onChange={setFrom} />
              </div>
              <div className="w-60">
                <DateTimeField id="history-to" label="Hasta" value={to} onChange={setTo} />
              </div>
              <div className="w-48">
                <DateTimeField id="history-day" label="Día" withTime={false} value={day} onChange={onDayChange} />
              </div>
              <Button type="submit">
                Consultar
              </Button>
            </div>
            <div className="flex flex-wrap items-center gap-2" role="group" aria-label="Atajos de fecha">
              {SHORTCUTS.map((s) => (
                <Button key={s.id} type="button" variant="outline" size="sm" onClick={() => applyPreset(shortcutRange(s.id, Date.now()))}>
                  {s.label}
                </Button>
              ))}
            </div>
            {formError && <InlineError>{formError}</InlineError>}
          </form>

          {data && (
            <div className="flex flex-col gap-1 text-sm" role="status">
              <p className="flex items-start gap-1.5">
                <IconInfoCircle className="mt-0.5 size-4 shrink-0 text-info" stroke={1.5} aria-hidden />
                <span>
                  {data.resolution === 'raw'
                    ? 'Detalle cada 30 s.'
                    : `Resumen por hora: el detalle de más de ${data.detail_retention_days ?? DETAIL_RETENTION_DAYS} días ya no se conserva.`}
                </span>
              </p>
              {startsBeforeData && availableFrom !== null && (
                <p className="pl-5.5 text-text-muted">
                  Hay datos de este host desde {formatFullDate(new Date(availableFrom).toISOString())}.
                </p>
              )}
            </div>
          )}

          <div className="flex flex-wrap items-center gap-2">
            <Button type="button" variant="outline" size="sm" disabled={exporting !== null} onClick={() => void exportCsv('metrics')}>
              <IconDownload stroke={1.5} aria-hidden />
              {exporting === 'metrics' ? 'Exportando…' : 'Exportar CSV (métricas)'}
            </Button>
            <Button type="button" variant="outline" size="sm" disabled={exporting !== null} onClick={() => void exportCsv('disks')}>
              <IconDownload stroke={1.5} aria-hidden />
              {exporting === 'disks' ? 'Exportando…' : 'Exportar CSV (discos)'}
            </Button>
          </div>
          {exportError && <InlineError>{exportError}</InlineError>}

          <div aria-busy={history.loading}>
            <HostCharts view={view} metrics={data} error={history.error} />
          </div>
        </>
      )}
    </div>
  )
}
