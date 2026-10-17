import { ConfirmDialog } from '@/components/ui/confirm-dialog'
import { useUserMutation } from '@/features/users/hooks/useUserMutation'
import { revokeHost } from '../services/hosts.service'
import type { HostDetail } from '../types/host.types'

interface RevokeHostDialogProps {
  /** El host a deshabilitar; null = cerrado. */
  host: HostDetail | null
  onClose: () => void
  onDone: () => void
}

export function RevokeHostDialog({ host, onClose, onDone }: RevokeHostDialogProps) {
  const mutation = useUserMutation()

  async function confirm() {
    if (host && (await mutation.run(() => revokeHost(host.id)))) onDone()
  }

  return (
    <ConfirmDialog
      open={host !== null}
      title={`Deshabilitar "${host?.hostname ?? ''}"`}
      description="Dejará de sondearse y sus alertas se resuelven. Podrás reactivarlo más adelante con un token nuevo."
      confirmLabel="Deshabilitar"
      destructive
      busy={mutation.busy}
      error={mutation.error}
      onConfirm={() => void confirm()}
      onClose={() => {
        mutation.clearError()
        onClose()
      }}
    />
  )
}
