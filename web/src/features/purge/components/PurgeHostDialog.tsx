import { useState } from 'react'
import { ConfirmDialog } from '@/components/ui/confirm-dialog'
import { FormField } from '@/components/ui/form-field'
import { purgeHost } from '../services/purge.service'
import { usePurgeRun } from '../hooks/usePurgeRun'
import { hostDeletedRowsOf, mapPurgeHostError, purgeNameMatches } from '../utils/purge'
import { PurgeJobDialog } from './PurgeJobDialog'

interface PurgeHostDialogProps {
  host: { id: string; hostname: string } | null
  onClose: () => void
  /** Al cerrar con la baja terminada, o cuando termina si se cerro antes. */
  onDone: () => void
}

/** Baja de un host: borra el host y todos sus datos en segundo plano. Irreversible; exige teclear el nombre exacto. */
export function PurgeHostDialog({ host, onClose, onDone }: PurgeHostDialogProps) {
  // El campo vive en un hijo que se monta con el dialogo abierto: cada apertura empieza vacia.
  return host ? <PurgeHostForm host={host} onClose={onClose} onDone={onDone} /> : null
}

function PurgeHostForm({ host, onClose, onDone }: { host: { id: string; hostname: string } } & Omit<PurgeHostDialogProps, 'host'>) {
  const [typed, setTyped] = useState('')
  const run = usePurgeRun(() => purgeHost(host.id, typed), mapPurgeHostError, onDone, onClose)

  if (run.job) {
    const rows = hostDeletedRowsOf(run.job)
    return (
      <PurgeJobDialog
        job={run.job}
        pollError={run.pollError}
        retryError={run.error}
        retrying={run.busy}
        runningTitle={`Eliminando "${host.hostname}"`}
        doneTitle="Baja completada"
        doneContent={rows !== null && <p className="text-sm tabular-nums">Filas de métricas borradas: {rows.toLocaleString('es-PE')}</p>}
        onRetry={() => void run.start()}
        onClose={run.close}
      />
    )
  }

  return (
    <ConfirmDialog
      open
      title={`Eliminar datos de "${host.hostname}"`}
      description="Se borra de forma definitiva el host y sus métricas, el resumen horario, el historial, los discos y los chequeos. No se borran los respaldos de la base de datos (se rotan a los 14 días) ni la auditoría. Esta acción no se puede deshacer."
      confirmLabel="Eliminar datos"
      destructive
      busy={run.busy}
      error={run.error}
      confirmDisabled={!purgeNameMatches(typed, host.hostname)}
      onConfirm={() => void run.start()}
      onClose={onClose}
    >
      <FormField
        id="purge-host-name"
        label={`Escribe "${host.hostname}" para confirmar`}
        value={typed}
        onChange={(e) => setTyped(e.target.value)}
        autoComplete="off"
        autoFocus
      />
    </ConfirmDialog>
  )
}
