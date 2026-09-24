import { SelectField } from '@/components/ui/select-field'
import { useGroups } from '@/features/groups/hooks/useGroups'
import { useTags } from '@/features/hosts/hooks/useTags'
import type { ScopeType } from '../types/alert.types'
import { SCOPE_TYPE_OPTIONS, type ScopeDraft } from '../utils/scope'

interface ScopeFieldsProps {
  /** Prefijo de ids (dos formularios no comparten ids). */
  id: string
  value: ScopeDraft
  onChange: (next: ScopeDraft) => void
  error?: string | null
}

/** Con el valor actual siempre presente (aunque ya no exista en el catálogo) para no perder la selección. */
function withCurrent(values: readonly string[], current: string): { value: string; label: string }[] {
  const all = current && !values.includes(current) ? [current, ...values] : values
  return all.map((v) => ({ value: v, label: v }))
}

/** Selector de alcance (todos | grupo | etiqueta), compartido por reglas y ventanas de mantenimiento. */
export function ScopeFields({ id, value, onChange, error }: ScopeFieldsProps) {
  const { groups } = useGroups()
  const tags = useTags()
  const groupOptions = (groups?.items ?? []).map((g) => ({ value: g.id, label: g.name }))

  return (
    <div className="grid gap-3">
      <SelectField
        id={`${id}-scope`}
        label="Alcance"
        value={value.type}
        onChange={(t) => onChange({ ...value, type: t as ScopeType })}
        options={SCOPE_TYPE_OPTIONS}
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
      {value.type === 'tag' && (
        <SelectField
          id={`${id}-tag`}
          label="Etiqueta"
          value={value.tag}
          onChange={(tag) => onChange({ ...value, tag })}
          options={withCurrent(
            tags.map((t) => t.name),
            value.tag,
          )}
          placeholder={tags.length === 0 ? 'No hay etiquetas' : 'Elige una etiqueta'}
          error={error}
        />
      )}
    </div>
  )
}
