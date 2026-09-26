import { useState } from 'react'
import type { ShrinkDetails } from '../types/retention.types'
import { parseShrink } from '../utils/retention'

/**
 * Guardado de retencion con confirmacion al reducir: `run(op, confirm)` llama a `op(confirm)`; si el servidor
 * responde 409 `shrink_needs_confirm` deja los datos en `shrink` (abre el dialogo) y devuelve false.
 * Reenviar con `run(op, true)` tras la confirmacion del usuario.
 */
export function useRetentionSave() {
  const [shrink, setShrink] = useState<ShrinkDetails | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function run(op: (confirmShrink: boolean) => Promise<unknown>, confirmShrink: boolean): Promise<boolean> {
    setBusy(true)
    setError(null)
    try {
      await op(confirmShrink)
      setShrink(null)
      return true
    } catch (err) {
      const s = parseShrink(err)
      if (s) setShrink(s)
      else setError(err instanceof Error ? err.message : 'Error desconocido')
      return false
    } finally {
      setBusy(false)
    }
  }

  return { shrink, busy, error, run, cancelShrink: () => setShrink(null) }
}
