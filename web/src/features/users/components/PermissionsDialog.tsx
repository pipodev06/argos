import { useState, type FormEvent } from 'react'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { InlineError } from '@/components/ui/inline-error'
import { Input } from '@/components/ui/input'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { useGroups } from '@/features/groups/hooks/useGroups'
import { useUserMutation } from '../hooks/useUserMutation'
import { updateUser } from '../services/users.service'
import type { ManagedUser } from '../types/user.types'
import {
  filterGroups,
  formFromUser,
  GROUP_SEARCH_THRESHOLD,
  isSameForm,
  MAX_USER_GROUPS,
  toggleGroupId,
  toPayload,
} from '../utils/permissions'

interface PermissionsDialogProps {
  user: ManagedUser | null
  onClose: () => void
  onSaved: () => void
}

export function PermissionsDialog(props: PermissionsDialogProps) {
  return (
    <Dialog open={props.user !== null} onOpenChange={(next) => !next && props.onClose()}>
      <DialogContent>{props.user && <PermissionsForm {...props} user={props.user} />}</DialogContent>
    </Dialog>
  )
}

function PermissionsForm({ user, onClose, onSaved }: PermissionsDialogProps & { user: ManagedUser }) {
  const [initial] = useState(() => formFromUser(user))
  const [form, setForm] = useState(initial)
  const [query, setQuery] = useState('')
  const { groups: data, error: groupsError, loading } = useGroups()
  const { error, busy, run } = useUserMutation()
  const groups = data?.items ?? []
  const shown = filterGroups(groups, query)

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    if (isSameForm(form, initial)) return onClose()
    const ok = await run(
      () => updateUser(user.id, toPayload(form)),
      (err) => (err.code === 'invalid_scope' ? 'Esa combinación de permisos no es válida para este usuario.' : null),
    )
    if (ok) onSaved()
  }

  return (
    <form onSubmit={(e) => void handleSubmit(e)} className="grid gap-4">
      <DialogHeader>
        <DialogTitle>Permisos de {user.username}</DialogTitle>
        <DialogDescription>El cambio se aplica en su próxima petición.</DialogDescription>
      </DialogHeader>
      <RadioGroup
        aria-label="Acceso a grupos"
        value={form.allGroups ? 'all' : 'some'}
        onValueChange={(v) => setForm({ ...form, allGroups: v === 'all' })}
      >
        <label htmlFor="perm-all" className="flex items-center gap-2 text-sm">
          <RadioGroupItem id="perm-all" value="all" />
          Todos los grupos
        </label>
        <label htmlFor="perm-some" className="flex items-center gap-2 text-sm">
          <RadioGroupItem id="perm-some" value="some" />
          Solo grupos seleccionados
        </label>
      </RadioGroup>
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
          {data && groups.length === 0 && <p className="text-sm text-text-muted">Aún no hay grupos.</p>}
          {data && groups.length > 0 && shown.length === 0 && <p className="text-sm text-text-muted">Ningún grupo coincide.</p>}
          <div className="grid max-h-56 gap-2 overflow-y-auto">
            {shown.map((g) => (
              <label key={g.id} htmlFor={`perm-group-${g.id}`} className="flex items-center gap-2 text-sm">
                <Checkbox
                  id={`perm-group-${g.id}`}
                  checked={form.groupIds.includes(g.id)}
                  onCheckedChange={(v) => setForm({ ...form, groupIds: toggleGroupId(form.groupIds, g.id, v === true) })}
                />
                <span className="truncate">{g.name}</span>
              </label>
            ))}
          </div>
          <p className="text-xs text-text-muted">
            Los hosts sin grupo solo los ve un usuario con acceso a todos los grupos.
          </p>
        </fieldset>
      )}
      <div className="grid gap-1">
        <label htmlFor="perm-inventory" className="flex items-center gap-2 text-sm font-medium text-text">
          <Checkbox
            id="perm-inventory"
            aria-describedby="perm-inventory-hint"
            checked={form.canInventory}
            onCheckedChange={(v) => setForm({ ...form, canInventory: v === true })}
          />
          Ver procesos y servicios
        </label>
        <p id="perm-inventory-hint" className="text-xs text-text-muted">
          Dato sensible. Solo aplica a los hosts que el usuario puede ver.
        </p>
      </div>
      {error && <InlineError>{error}</InlineError>}
      <DialogFooter>
        <Button type="button" variant="ghost" onClick={onClose}>
          Cancelar
        </Button>
        <Button type="submit" disabled={busy}>
          {busy ? 'Guardando...' : 'Guardar'}
        </Button>
      </DialogFooter>
    </form>
  )
}
