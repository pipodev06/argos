import { useState } from 'react'
import { ConfirmDialog } from '@/components/ui/confirm-dialog'
import { FormField } from '@/components/ui/form-field'
import { useUserMutation } from '@/features/users/hooks/useUserMutation'
import { purgeHost } from '../services/purge.service'
import { mapPurgeHostError, purgeNameMatches } from '../utils/purge'

interface PurgeHostDialogProps {
  host: { id: string; hostname: string } | null
  onClose: () => void
  onDone: () => void
}

/** Baja de un host: borra el host y todos sus datos. Irreversible; exige teclear el nombre exacto. */
export function PurgeHostDialog({ host, onClose, onDone }: PurgeHostDialogProps) {
  // El campo vive en un hijo que se monta con el dialogo abierto: cada apertura empieza vacia.
  return host ? <PurgeHostForm host={host} onClose={onClose} onDone={onDone} /> : null
}

function PurgeHostForm({ host, onClose, onDone }: { host: { id: string; hostname: string } } & Omit<PurgeHostDialogProps, 'host'>) {
  const [typed, setTyped] = useState('')
  const { error, busy, run } = useUserMutation()

  async function confirm() {
    if (await run(() => purgeHost(host.id, typed), mapPurgeHostError)) onDone()
  }

  return (
    <ConfirmDialog
      open
      title={`Eliminar datos de "${host.hostname}"`}
      description="Se borra de forma definitiva el host y sus métricas, el resumen horario, el historial, los discos y los chequeos. No se borran los respaldos de la base de datos (se rotan a los 14 días) ni la auditoría. Esta acción no se puede deshacer."
      confirmLabel="Eliminar datos"
      busy={busy}
      error={error}
      confirmDisabled={!purgeNameMatches(typed, host.hostname)}
      onConfirm={() => void confirm()}
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
