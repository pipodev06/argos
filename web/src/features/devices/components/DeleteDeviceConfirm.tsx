import { ConfirmDialog } from '@/components/ui/confirm-dialog'
import { PurgeJobDialog } from '@/features/purge/components/PurgeJobDialog'
import { usePurgeRun } from '@/features/purge/hooks/usePurgeRun'
import type { Device } from '../types/device.types'
import { deleteDevice } from '../services/devices.service'
import { mapDeviceError } from '../utils/deviceForm'

interface DeleteDeviceConfirmProps {
  /** El dispositivo a eliminar; null = cerrado. */
  device: Device | null
  onClose: () => void
  /** Al cerrar con la baja terminada, o cuando termina si se cerro antes. */
  onDeleted: () => void
}

export function DeleteDeviceConfirm({ device, onClose, onDeleted }: DeleteDeviceConfirmProps) {
  // El estado de la baja vive en un hijo que se monta con el dialogo abierto: cada apertura empieza limpia.
  return device ? <DeleteDeviceForm device={device} onClose={onClose} onDeleted={onDeleted} /> : null
}

function DeleteDeviceForm({ device, onClose, onDeleted }: { device: Device } & Omit<DeleteDeviceConfirmProps, 'device'>) {
  const run = usePurgeRun(() => deleteDevice(device.id), mapDeviceError, onDeleted, onClose)

  if (run.job) {
    return (
      <PurgeJobDialog
        job={run.job}
        pollError={run.pollError}
        retryError={run.error}
        retrying={run.busy}
        runningTitle={`Eliminando "${device.name}"`}
        doneTitle="Dispositivo eliminado"
        doneContent={null}
        onRetry={() => void run.start()}
        onClose={run.close}
      />
    )
  }

  return (
    <ConfirmDialog
      open
      title={`Eliminar "${device.name}"`}
      description="El dispositivo se elimina de forma definitiva y deja de sondearse."
      confirmLabel="Eliminar"
      busy={run.busy}
      error={run.error}
      onConfirm={() => void run.start()}
      onClose={onClose}
    />
  )
}
