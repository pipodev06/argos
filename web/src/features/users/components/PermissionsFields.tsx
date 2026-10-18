import { useState } from 'react'
import { Checkbox } from '@/components/ui/checkbox'
import { InlineError } from '@/components/ui/inline-error'
import { Input } from '@/components/ui/input'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { useGroups } from '@/features/groups/hooks/useGroups'
import {
  filterGroups,
  GROUP_SEARCH_THRESHOLD,
  MAX_USER_GROUPS,
  toggleGroupId,
  type PermissionsForm,
} from '../utils/permissions'

interface PermissionsFieldsProps {
  idPrefix: string
  form: PermissionsForm
  onChange: (next: PermissionsForm) => void
}

/** Ámbito por grupos y permisos de un viewer; compartido por el alta y el diálogo de permisos. */
export function PermissionsFields({ idPrefix, form, onChange }: PermissionsFieldsProps) {
  const [query, setQuery] = useState('')
  const { groups: data, error: groupsError, loading } = useGroups()
  const groups = data?.items ?? []
  const shown = filterGroups(groups, query)
  const id = (s: string) => `${idPrefix}-${s}`

  return (
    <>
      <RadioGroup
        aria-label="Acceso a grupos"
        value={form.allGroups ? 'all' : 'some'}
        onValueChange={(v) => onChange({ ...form, allGroups: v === 'all' })}
      >
        <label htmlFor={id('all')} className="flex items-center gap-2 text-sm">
          <RadioGroupItem id={id('all')} value="all" />
          Todos los grupos (ve todos los clientes)
        </label>
        <label htmlFor={id('some')} className="flex items-center gap-2 text-sm">
          <RadioGroupItem id={id('some')} value="some" />
          Solo grupos seleccionados
        </label>
      </RadioGroup>
      {form.allGroups && (
        <p className="text-xs text-text-muted">Da acceso a TODOS los clientes, incluidos los hosts sin grupo y los que se creen después.</p>
      )}
      {!form.allGroups && (
        <fieldset className="grid gap-2">
          <legend className="text-sm font-medium text-text">
            Grupos ({form.groupIds.length} de máx. {MAX_USER_GROUPS})
          </legend>
          {groups.length > GROUP_SEARCH_THRESHOLD && (
            <Input
              aria-label="Buscar grupos"
              placeholder="Buscar grupo"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              maxLength={64}
            />
          )}
          {groupsError && <InlineError>{groupsError}</InlineError>}
          {loading && <p className="text-xs text-text-muted">Cargando grupos...</p>}
          {data && groups.length === 0 && <p className="text-sm text-text-muted">Sin grupos</p>}
          {data && groups.length > 0 && shown.length === 0 && <p className="text-sm text-text-muted">Ningún grupo coincide.</p>}
          <div className="grid max-h-56 gap-2 overflow-y-auto">
            {shown.map((g) => (
              <label key={g.id} htmlFor={id(`group-${g.id}`)} className="flex items-center gap-2 text-sm">
                <Checkbox
                  id={id(`group-${g.id}`)}
                  checked={form.groupIds.includes(g.id)}
                  onCheckedChange={(v) => onChange({ ...form, groupIds: toggleGroupId(form.groupIds, g.id, v === true) })}
                />
                <span className="truncate">{g.name}</span>
              </label>
            ))}
          </div>
        </fieldset>
      )}
      <PermCheck
        id={id('inventory')}
        label="Ver procesos y servicios"
        checked={form.canInventory}
        onChange={(v) => onChange({ ...form, canInventory: v })}
      />
      <PermCheck
        id={id('updates')}
        label="Ver parches del SO"
        checked={form.canUpdates}
        onChange={(v) => onChange({ ...form, canUpdates: v })}
      />
      <PermCheck
        id={id('ack')}
        label="Reconocer alertas"
        checked={form.canAck}
        onChange={(v) => onChange({ ...form, canAck: v })}
      />
    </>
  )
}

function PermCheck({ id, label, checked, onChange }: { id: string; label: string; checked: boolean; onChange: (v: boolean) => void }) {
  return (
    <label htmlFor={id} className="flex items-center gap-2 text-sm font-medium text-text">
      <Checkbox id={id} checked={checked} onCheckedChange={(v) => onChange(v === true)} />
      {label}
    </label>
  )
}
