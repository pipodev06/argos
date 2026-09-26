import { ConfirmDialog } from '@/components/ui/confirm-dialog'
import type { ShrinkDetails } from '../types/retention.types'
import { formatBytes } from '../utils/retention'

interface ShrinkConfirmDialogProps {
  /** null = cerrado. */
  shrink: ShrinkDetails | null
  busy: boolean
  error: string | null
  onConfirm: () => void
  onClose: () => void
}

/** Confirmacion al reducir la retencion: el servidor avisa (409) de cuanto detalle se borraria. */
export function ShrinkConfirmDialog({ shrink, busy, error, onConfirm, onClose }: ShrinkConfirmDialogProps) {
  return (
    <ConfirmDialog
      open={shrink !== null}
      title="Reducir la retención borra datos"
      description={
        shrink && (
          <>
            Se borrará el detalle más antiguo: hasta {shrink.days_lost.toLocaleString('es-PE')} días, en{' '}
            {shrink.chunks.toLocaleString('es-PE')} bloques (≈ {formatBytes(shrink.est_bytes)}). El borrado no se puede deshacer.
          </>
        )
      }
      confirmLabel="Reducir y borrar"
      busy={busy}
      error={error}
      onConfirm={onConfirm}
      onClose={onClose}
    />
  )
}
