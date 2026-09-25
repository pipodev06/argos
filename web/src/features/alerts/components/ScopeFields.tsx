import { SelectField } from '@/components/ui/select-field'
import { useAllDevices } from '@/features/devices/hooks/useDevices'
import { useGroups } from '@/features/groups/hooks/useGroups'
import type { ScopeType } from '../types/alert.types'
import { PING_SCOPE_TYPE_OPTIONS, SCOPE_TYPE_OPTIONS, type ScopeDraft } from '../utils/scope'

interface ScopeFieldsProps {
  /** Prefijo de ids (dos formularios no comparten ids). */
  id: string
  value: ScopeDraft
  onChange: (next: ScopeDraft) => void
  error?: string | null
  /** Chequeo ping: el alcance son dispositivos (todos | grupo | un dispositivo). */
  ping?: boolean
}

/** Selector de dispositivo; solo se monta con el alcance "Un dispositivo" (carga la lista entonces). */
function DeviceScopeSelect({ id, value, onChange, error }: Omit<ScopeFieldsProps, 'ping'>) {
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

/** Selector de alcance (todos | grupo, y un dispositivo en ping), compartido por reglas, mantenimiento y chequeos. */
export function ScopeFields({ id, value, onChange, error, ping = false }: ScopeFieldsProps) {
  const { groups } = useGroups()
  const groupOptions = (groups?.items ?? []).map((g) => ({ value: g.id, label: g.name }))

  return (
    <div className="grid gap-3">
      <SelectField
        id={`${id}-scope`}
        label="Alcance"
        value={value.type}
        onChange={(t) => onChange({ ...value, type: t as ScopeType })}
        options={ping ? PING_SCOPE_TYPE_OPTIONS : SCOPE_TYPE_OPTIONS}
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
    </div>
  )
}
