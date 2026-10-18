import { SelectField } from '@/components/ui/select-field'
import { useAllDevices, useCollectors } from '@/features/devices/hooks/useDevices'
import { useGroups } from '@/features/groups/hooks/useGroups'
import type { ScopeType } from '../types/alert.types'
import { CHECK_SCOPE_TYPE_OPTIONS, PING_SCOPE_TYPE_OPTIONS, SCOPE_TYPE_OPTIONS, scopeOptions, type ScopeDraft } from '../utils/scope'

interface ScopeFieldsProps {
  /** Prefijo de ids (dos formularios no comparten ids). */
  id: string
  value: ScopeDraft
  onChange: (next: ScopeDraft) => void
  error?: string | null
  /** Chequeo ping: el alcance son dispositivos (todos | grupo | un dispositivo). */
  ping?: boolean
  /** Chequeo de servicio: ademas de todos | grupo, puede apuntar a un host concreto. */
  hostScope?: boolean
  /** Lista explicita de tipos de alcance (reglas y mantenimiento); tiene prioridad sobre `ping` y `hostScope`. */
  types?: readonly ScopeType[]
}

/** Selector de dispositivo; solo se monta con el alcance "Un dispositivo" (carga la lista entonces). */
function DeviceScopeSelect({ id, value, onChange, error }: Omit<ScopeFieldsProps, 'ping' | 'hostScope' | 'types'>) {
  const { data: devices, error: loadError } = useAllDevices()
  const options = (devices ?? []).map((d) => ({ value: d.id, label: d.name }))
  return (
    <SelectField
      id={`${id}-device`}
      label="Dispositivo"
      value={value.deviceId ?? ''}
      onChange={(deviceId) => onChange({ ...value, deviceId })}
      options={options}
      placeholder={devices ? (options.length ? 'Elige un dispositivo' : 'No hay dispositivos') : 'Cargando…'}
      error={error ?? loadError}
    />
  )
}

/** Selector de host con agente; solo se monta con el alcance "Un host" (carga la lista entonces). */
function HostScopeSelect({ id, value, onChange, error }: Omit<ScopeFieldsProps, 'ping' | 'hostScope' | 'types'>) {
  const { data: hosts, error: loadError } = useCollectors()
  const options = (hosts ?? []).map((h) => ({ value: h.id, label: h.name }))
  return (
    <SelectField
      id={`${id}-host`}
      label="Host"
      value={value.hostId ?? ''}
      onChange={(hostId) => onChange({ ...value, hostId })}
      options={options}
      placeholder={hosts ? (options.length ? 'Elige un host' : 'No hay hosts con agente') : 'Cargando…'}
      error={error ?? loadError}
    />
  )
}

/** Selector de alcance (todos | grupo, un dispositivo en ping y un host en chequeos de servicio), compartido por reglas, mantenimiento y chequeos. */
export function ScopeFields({ id, value, onChange, error, ping = false, hostScope = false, types }: ScopeFieldsProps) {
  const { groups } = useGroups()
  const groupOptions = (groups?.items ?? []).map((g) => ({ value: g.id, label: g.name }))

  // Alcance y su objetivo (grupo, host, dispositivo...) lado a lado: el objetivo no queda colgando debajo.
  return (
    <div className="grid items-start gap-3 sm:grid-cols-2">
      <SelectField
        id={`${id}-scope`}
        label="Alcance"
        value={value.type}
        onChange={(t) => onChange({ ...value, type: t as ScopeType })}
        options={types ? scopeOptions(types) : ping ? PING_SCOPE_TYPE_OPTIONS : hostScope ? CHECK_SCOPE_TYPE_OPTIONS : SCOPE_TYPE_OPTIONS}
      />
      {value.type === 'group' && (
        <SelectField
          id={`${id}-group`}
          label="Grupo"
          value={value.groupId}
          onChange={(groupId) => onChange({ ...value, groupId })}
          options={groupOptions}
          placeholder={groups ? 'Elige un grupo' : 'Cargando…'}
          error={error}
        />
      )}
      {value.type === 'device' && <DeviceScopeSelect id={id} value={value} onChange={onChange} error={error} />}
      {value.type === 'host' && <HostScopeSelect id={id} value={value} onChange={onChange} error={error} />}
    </div>
  )
}
