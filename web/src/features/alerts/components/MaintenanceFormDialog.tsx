import { useState, type FormEvent } from 'react'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { FormField } from '@/components/ui/form-field'
import { InlineError } from '@/components/ui/inline-error'
import { useUserMutation } from '@/features/users/hooks/useUserMutation'
import { createMaintenance, updateMaintenance } from '../services/alerts.service'
import type { MaintenanceWindow } from '../types/alert.types'
import {
  buildMaintenanceInput,
  maintenanceToDraft,
  type MaintenanceDraft,
  type MaintenanceErrors,
} from '../utils/maintenanceForm'
import { MAX_NAME } from '../utils/ruleForm'
import { ScopeFields } from './ScopeFields'
import { MAINTENANCE_SCOPE_TYPES } from '../utils/scope'

interface MaintenanceFormDialogProps {
  /** null = crear; una ventana = editar. */
  window: MaintenanceWindow | null
  open: boolean
  onClose: () => void
  onSaved: () => void
}

export function MaintenanceFormDialog(props: MaintenanceFormDialogProps) {
  return (
    <Dialog open={props.open} onOpenChange={(next) => !next && props.onClose()}>
      <DialogContent className="max-h-[90dvh] overflow-y-auto">{props.open && <MaintenanceForm {...props} />}</DialogContent>
    </Dialog>
  )
}

function MaintenanceForm({ window: win, onClose, onSaved }: MaintenanceFormDialogProps) {
  const [original] = useState<MaintenanceDraft | null>(() => (win ? maintenanceToDraft(win) : null))
  const [draft, setDraft] = useState<MaintenanceDraft>(() => original ?? maintenanceToDraft(null))
  const [errors, setErrors] = useState<MaintenanceErrors>({})
  const { error, busy, run } = useUserMutation()
  const set = <K extends keyof MaintenanceDraft>(key: K, value: MaintenanceDraft[K]) => setDraft((d) => ({ ...d, [key]: value }))

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    const built = buildMaintenanceInput(draft, original)
    if ('errors' in built) return setErrors(built.errors)
    setErrors({})
    const { input } = built
    const ok = await run(() => (win ? updateMaintenance(win.id, input) : createMaintenance(input)))
    if (ok) onSaved()
  }

  return (
    <form onSubmit={(e) => void handleSubmit(e)} className="grid gap-4" noValidate>
      <DialogHeader>
        <DialogTitle>{win ? 'Editar ventana' : 'Nueva ventana de mantenimiento'}</DialogTitle>
        <DialogDescription>
          Durante la ventana las alertas de los hosts del alcance se muestran como "En mantenimiento" y no se notifican.
        </DialogDescription>
      </DialogHeader>
      <FormField
        id="maint-name"
        label="Nombre"
        value={draft.name}
        onChange={(e) => set('name', e.target.value)}
        maxLength={MAX_NAME}
        autoComplete="off"
        autoFocus
        error={errors.name}
      />
      <div className="grid gap-3 sm:grid-cols-2">
        <FormField
          id="maint-start"
          type="datetime-local"
          label="Inicio"
          value={draft.startsAt}
          onChange={(e) => set('startsAt', e.target.value)}
          error={errors.startsAt}
        />
        <FormField
          id="maint-end"
          type="datetime-local"
          label="Fin"
          value={draft.endsAt}
          onChange={(e) => set('endsAt', e.target.value)}
          hint="Máximo 31 días; sobre todos los hosts, máximo 24 horas."
          error={errors.endsAt}
        />
      </div>
      <ScopeFields id="maint" types={MAINTENANCE_SCOPE_TYPES} value={draft.scope} onChange={(s) => set('scope', s)} error={errors.scope} />
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
