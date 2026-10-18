import { useMemo, useState } from 'react'
import { DownloadIcon, InfoIcon } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { DateTimeField } from '@/components/ui/date-time-picker'
import { InlineError } from '@/components/ui/inline-error'
import { ApiError } from '@/lib/api'
import { useNow } from '@/hooks/useNow'
import { useHostHistory } from '../hooks/useHostHistory'
import { downloadHostHistoryCsv } from '../services/hosts.service'
import {
  DETAIL_RETENTION_DAYS,
  SHORTCUTS,
  historyView,
  parseLocalInput,
  shortcutRange,
  toLocalInput,
  validateRange,
  type DateRange,
  type HistorySeries,
} from '../utils/history'
import { formatFullDate } from '../utils/format'
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

/** Rango personalizado de la pestaña Métricas: gráficas de un rango elegido (no relativo a "ahora") y exportación CSV. */
export function HostHistoryPanel({ id, hostname }: { id: string; hostname: string }) {
  const [initial] = useState(() => shortcutRange('yesterday', Date.now()))
  const [applied, setApplied] = useState<DateRange>(initial)
  const [from, setFrom] = useState(() => toLocalInput(initial.fromMs))
  const [to, setTo] = useState(() => toLocalInput(initial.toMs))
  const [formError, setFormError] = useState<string | null>(null)
  const [exporting, setExporting] = useState<HistorySeries | null>(null)
  const [exportError, setExportError] = useState<string | null>(null)
  const history = useHostHistory(id, applied)
  // Tope de los calendarios: ahora (no hay datos futuros); un "Hasta" posterior se recorta solo.
  const maxDate = new Date(useNow(30_000))
  const notFound = history.errorStatus === 404

  function apply(range: DateRange, now: number) {
    const err = validateRange(range, now, history.data?.detail_retention_days)
    setFormError(err)
    setExportError(null)
    if (!err) setApplied(range)
  }

  function applyPreset(range: DateRange, now: number) {
    setFrom(toLocalInput(range.fromMs))
    setTo(toLocalInput(range.toMs))
    apply(range, now)
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
    <div className="flex flex-col gap-3">
      {notFound ? (
        <InlineError>Host no encontrado.</InlineError>
      ) : (
        <>
          <form
            className="flex flex-col gap-3 rounded-md border border-border bg-surface p-4"
            onSubmit={(e) => {
              e.preventDefault()
              apply({ fromMs: parseLocalInput(from), toMs: parseLocalInput(to) }, Date.now())
            }}
            noValidate
          >
            <div className="flex flex-wrap items-end gap-2">
              <div className="w-60">
                <DateTimeField id="history-from" label="Desde" value={from} onChange={setFrom} maxDate={maxDate} />
              </div>
              <div className="w-60">
                <DateTimeField id="history-to" label="Hasta" value={to} onChange={setTo} maxDate={maxDate} />
              </div>
              <Button type="submit">
                Consultar
              </Button>
            </div>
            <div className="flex flex-wrap items-center gap-2" role="group" aria-label="Atajos de fecha">
              {SHORTCUTS.map((s) => (
                <Button key={s.id} type="button" variant="outline" size="sm" onClick={() => {
                    const now = Date.now()
                    applyPreset(shortcutRange(s.id, now), now)
                  }}>
                  {s.label}
                </Button>
              ))}
            </div>
            {formError && <InlineError>{formError}</InlineError>}
          </form>

          {data && (
            <div className="flex flex-col gap-1 text-sm" role="status">
              <p className="flex items-start gap-1.5">
                <InfoIcon className="mt-0.5 size-4 shrink-0 text-info" strokeWidth={1.5} aria-hidden />
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
              <DownloadIcon strokeWidth={1.5} aria-hidden />
              {exporting === 'metrics' ? 'Exportando…' : 'Exportar CSV (métricas)'}
            </Button>
            <Button type="button" variant="outline" size="sm" disabled={exporting !== null} onClick={() => void exportCsv('disks')}>
              <DownloadIcon strokeWidth={1.5} aria-hidden />
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
