import { useState, type FormEvent } from 'react'
import { Card } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { FormField } from '@/components/ui/form-field'
import { InlineError } from '@/components/ui/inline-error'
import { useRetentionSave } from '../hooks/useRetentionSave'
import { putRetention } from '../services/retention.service'
import type { RetentionSettings } from '../types/retention.types'
import { estimateWeight, formatBytes, parseDetailDays } from '../utils/retention'
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
    <Card role="region" aria-labelledby="ret-title" className="gap-3 px-4">
      <h2 id="ret-title" className="text-sm font-semibold">Retención de datos</h2>
      <form onSubmit={handleSubmit} className="grid max-w-md gap-3">
        <FormField
          id="ret-days"
          label="Detalle de 30 s (días)"
          inputMode="numeric"
          className="w-28"
          value={raw}
          onChange={(e) => {
            setRaw(e.target.value)
            setSaved(false)
          }}
          autoComplete="off"
          error={fieldError}
        />
        {weight && (
          <p className="text-xs text-text-muted tabular-nums" aria-live="polite">
            ≈ {formatBytes(weight.perHost)} por host
            {hostCount !== null && hostCount > 0 && <> · ≈ {formatBytes(weight.total)} en total</>}
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
    </Card>
  )
}
