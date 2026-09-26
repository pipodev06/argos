import { useState, type FormEvent } from 'react'
import { Button } from '@/components/ui/button'
import { FormField } from '@/components/ui/form-field'
import { InlineError } from '@/components/ui/inline-error'
import { useRetentionSave } from '../hooks/useRetentionSave'
import { putRetention } from '../services/retention.service'
import type { RetentionSettings } from '../types/retention.types'
import { estimateWeight, formatBytes, MAX_DETAIL_DAYS, MIN_DETAIL_DAYS, parseDetailDays, sourceLabel } from '../utils/retention'
import { ShrinkConfirmDialog } from './ShrinkConfirmDialog'

interface RetentionSectionProps {
  settings: RetentionSettings
  /** Total de hosts con agente (para la estimacion); null si aun no se conoce. */
  hostCount: number | null
  onSaved: () => void
}

/** Retencion global del detalle: valor efectivo y origen, formulario y estimacion de peso. */
export function RetentionSection({ settings, hostCount, onSaved }: RetentionSectionProps) {
  const [raw, setRaw] = useState(String(settings.detail_days))
  const [fieldError, setFieldError] = useState<string | null>(null)
  const [saved, setSaved] = useState(false)
  const save = useRetentionSave()

  const parsed = parseDetailDays(raw)
  const weight = 'days' in parsed ? estimateWeight(parsed.days, hostCount ?? 0) : null
  const dirty = 'days' in parsed && parsed.days !== settings.detail_days

  async function submit(confirm: boolean) {
    if (!('days' in parsed)) return setFieldError(parsed.error)
    setFieldError(null)
    setSaved(false)
    if (await save.run((c) => putRetention(parsed.days, c), confirm)) {
      setSaved(true)
      onSaved()
    }
  }

  function handleSubmit(e: FormEvent) {
    e.preventDefault()
    void submit(false)
  }

  return (
    <section aria-labelledby="ret-title" className="flex flex-col gap-4 rounded-md border border-border bg-surface p-4">
      <div className="flex flex-col gap-1">
        <h2 id="ret-title" className="text-sm font-semibold">Retención de datos</h2>
        <p className="text-sm text-text-muted">
          Cuántos días se conserva el detalle de 30 s de cada host; después solo queda el resumen por hora, que se
          conserva {settings.hourly_days} días. Los últimos {settings.hot_days} días de detalle no están comprimidos.
        </p>
      </div>
      <p className="text-sm tabular-nums">
        Valor actual: <span className="font-semibold">{settings.detail_days} días</span> · {sourceLabel(settings.source)}
      </p>
      <form onSubmit={handleSubmit} className="grid max-w-md gap-3">
        <FormField
          id="ret-days"
          label="Retención global de detalle (días)"
          inputMode="numeric"
          className="w-28"
          value={raw}
          onChange={(e) => {
            setRaw(e.target.value)
            setSaved(false)
          }}
          autoComplete="off"
          hint={`Entre ${MIN_DETAIL_DAYS} y ${MAX_DETAIL_DAYS} días. Rige desde el siguiente ciclo de mantenimiento, sin reiniciar.`}
          error={fieldError}
        />
        {weight && (
          <p className="text-sm tabular-nums" aria-live="polite">
            Peso estimado: ≈ {formatBytes(weight.perHost)} por host
            {hostCount !== null && <>, ≈ {formatBytes(weight.total)} con {hostCount.toLocaleString('es-PE')} hosts</>}.{' '}
            <span className="text-text-muted">Estimación (≈ 23 KB por host y día comprimido).</span>
          </p>
        )}
        {save.error && !save.shrink && <InlineError>{save.error}</InlineError>}
        {saved && <p role="status" className="text-sm">Retención guardada.</p>}
        <div>
          <Button type="submit" size="sm" disabled={save.busy || !dirty}>
            {save.busy ? 'Guardando...' : 'Guardar'}
          </Button>
        </div>
      </form>
      <ShrinkConfirmDialog
        shrink={save.shrink}
        busy={save.busy}
        error={save.error}
        onConfirm={() => void submit(true)}
        onClose={save.cancelShrink}
      />
    </section>
  )
}
