import { usePolledFetch } from '@/hooks/usePolledFetch'
import { listSnmpProfiles } from '../services/snmpProfiles.service'

/** Lista de la página de perfiles (con refresco). */
export function useSnmpProfiles() {
  return usePolledFetch(listSnmpProfiles, { intervalMs: 30_000 })
}

/** Opciones de un formulario (solo admin): una carga al montar, sin refresco. */
export function useSnmpProfileOptions() {
  return usePolledFetch(listSnmpProfiles, { intervalMs: 30_000, paused: true })
}
