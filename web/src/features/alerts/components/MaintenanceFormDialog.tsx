import { useState, type FormEvent } from 'react'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { DateTimeField } from '@/components/ui/date-time-picker'
import { FormField } from '@/components/ui/form-field'
import { FormHelp, type HelpItem } from '@/components/ui/form-help'
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

const MAINTENANCE_HELP: readonly HelpItem[] = [
  ['Ventana', 'Durante la ventana, las alertas de los hosts del alcance se muestran como "En mantenimiento" y no se notifican.'],
  ['Desde / Hasta', 'Máximo 31 días; sobre todos los hosts, máximo 24 horas.'],
]

export function MaintenanceFormDialog(props: MaintenanceFormDialogProps) {
  return (
    <Dialog open={props.open} onOpenChange={(next) => !next && props.onClose()}>
      <DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-2xl">{props.open && <MaintenanceForm {...props} />}</DialogContent>
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
    <form onSubmit={(e) => void handleSubmit(e)} className="grid gap-3" noValidate>
      <DialogHeader className="flex-row items-center gap-1">
        <DialogTitle>{win ? 'Editar ventana' : 'Nueva ventana de mantenimiento'}</DialogTitle>
        <FormHelp title="Mantenimiento" items={MAINTENANCE_HELP} />
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
        <DateTimeField id="maint-start" label="Inicio" value={draft.startsAt} onChange={(v) => set('startsAt', v)} error={errors.startsAt} />
        <DateTimeField id="maint-end" label="Fin" value={draft.endsAt} onChange={(v) => set('endsAt', v)} error={errors.endsAt} />
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
