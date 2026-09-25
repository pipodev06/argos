import { useState } from 'react'
import { IconAlertTriangle, IconX } from '@tabler/icons-react'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { InlineError } from '@/components/ui/inline-error'
import { bulkHosts } from '../services/hosts.service'
import { BulkDialog, type BulkMode } from './BulkDialog'

interface BulkBarProps {
  ids: string[]
  onClear: () => void
  /** Tras una accion aplicada: limpia la seleccion y recarga la lista. */
  onDone: () => void
}

/** Barra de lote (solo admin): reemplaza a los filtros mientras hay hosts seleccionados. */
export function BulkBar({ ids, onClear, onDone }: BulkBarProps) {
  const [mode, setMode] = useState<BulkMode | null>(null)
  const [confirmDisable, setConfirmDisable] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function disable() {
    setBusy(true)
    setError(null)
    try {
      await bulkHosts({ ids, action: 'disable' })
      setConfirmDisable(false)
      onDone()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Error desconocido')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div
      role="region"
      aria-label="Acciones en lote"
      className="flex flex-wrap items-center gap-2 rounded-md border border-border bg-muted px-3 py-2"
    >
      <span className="text-sm font-medium tabular-nums" aria-live="polite">
        {ids.length} {ids.length === 1 ? 'seleccionado' : 'seleccionados'}
      </span>
      <Button variant="outline" size="sm" onClick={() => setMode('set_group')}>Asignar grupo</Button>
      <Button variant="outline" size="sm" onClick={() => setConfirmDisable(true)}>Deshabilitar</Button>
      <Button variant="ghost" size="icon-sm" aria-label="Quitar selección" className="ml-auto" onClick={onClear}>
        <IconX stroke={1.5} aria-hidden />
      </Button>

      <BulkDialog
        mode={mode}
        ids={ids}
        onClose={() => setMode(null)}
        onDone={() => {
          setMode(null)
          onDone()
        }}
      />

      <AlertDialog
        open={confirmDisable}
        onOpenChange={(open) => {
          if (open) return
          setConfirmDisable(false)
          setError(null)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle className="flex items-center gap-2">
              <IconAlertTriangle className="size-5 shrink-0 text-danger" stroke={1.5} aria-hidden />
              Deshabilitar {ids.length} {ids.length === 1 ? 'host' : 'hosts'}
            </AlertDialogTitle>
            <AlertDialogDescription>
              Se revoca el token de cada agente y dejan de reportar. Esta acción es definitiva: para volver a
              monitorearlos hay que enrolarlos de nuevo.
            </AlertDialogDescription>
          </AlertDialogHeader>
          {error && <InlineError>{error}</InlineError>}
          <AlertDialogFooter>
            <AlertDialogCancel>Cancelar</AlertDialogCancel>
            <AlertDialogAction
              disabled={busy}
              onClick={(e) => {
                e.preventDefault()
                void disable()
              }}
            >
              {busy ? 'Deshabilitando...' : 'Deshabilitar'}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
