import { ConfirmDialog } from '@/components/ui/confirm-dialog'
import { useUserMutation } from '@/features/users/hooks/useUserMutation'
import { deleteDevice } from '../services/devices.service'
import type { Device } from '../types/device.types'
import { mapDeviceError } from '../utils/deviceForm'

interface DeleteDeviceConfirmProps {
  /** El dispositivo a eliminar; null = cerrado. */
  device: Device | null
  onClose: () => void
  onDeleted: () => void
}

export function DeleteDeviceConfirm({ device, onClose, onDeleted }: DeleteDeviceConfirmProps) {
  const del = useUserMutation()

  async function confirm() {
    if (device && (await del.run(() => deleteDevice(device.id), mapDeviceError))) onDeleted()
  }

  return (
    <ConfirmDialog
      open={device !== null}
      title={`Eliminar "${device?.name ?? ''}"`}
      description="El dispositivo se elimina de forma definitiva y deja de sondearse."
      confirmLabel="Eliminar"
      busy={del.busy}
      error={del.error}
      onConfirm={() => void confirm()}
      onClose={() => {
        del.clearError()
        onClose()
      }}
    />
  )
}
