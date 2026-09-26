import { useUserMutation } from '@/features/users/hooks/useUserMutation'
import { updateDevice } from '../services/devices.service'
import type { Device } from '../types/device.types'
import { mapDeviceError } from '../utils/deviceForm'

/** Habilita o deshabilita un dispositivo (PATCH enabled): uno deshabilitado pasa a habilitarse y al revés. */
export function useDeviceEnabled(onDone: () => void) {
  const mutation = useUserMutation()

  async function toggle(device: Pick<Device, 'id' | 'status'>) {
    if (await mutation.run(() => updateDevice(device.id, { enabled: device.status === 'disabled' }), mapDeviceError)) onDone()
  }

  return { toggle, busy: mutation.busy, error: mutation.error }
}
