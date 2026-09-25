import { useState, type FormEvent } from 'react'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { InlineError } from '@/components/ui/inline-error'
import { GroupSelect } from '@/features/groups/components/GroupSelect'
import { useGroups } from '@/features/groups/hooks/useGroups'
import { ApiError } from '@/lib/api'
import { setHostGroup } from '../services/hosts.service'
import type { GroupRef } from '../types/host.types'

interface EditHostDialogProps {
  host: { id: string; hostname: string; group: GroupRef | null } | null
  onClose: () => void
  onSaved: () => void
}

const ERRORS: Record<string, string> = {
  group_not_found: 'El grupo ya no existe.',
}

export function EditHostDialog({ host, onClose, onSaved }: EditHostDialogProps) {
  return (
    <Dialog open={host !== null} onOpenChange={(next) => !next && onClose()}>
      <DialogContent>{host && <EditForm host={host} onClose={onClose} onSaved={onSaved} />}</DialogContent>
    </Dialog>
  )
}

function EditForm({ host, onClose, onSaved }: { host: NonNullable<EditHostDialogProps['host']> } & Omit<EditHostDialogProps, 'host'>) {
  const [groupId, setGroupId] = useState<string | null>(host.group?.id ?? null)
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const { groups } = useGroups()

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setError(null)
    setSubmitting(true)
    try {
      if (groupId !== (host.group?.id ?? null)) await setHostGroup(host.id, groupId)
      onSaved()
    } catch (err) {
      const code = err instanceof ApiError ? err.code : ''
      setError(ERRORS[code] ?? (err instanceof Error ? err.message : 'Error desconocido'))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <form onSubmit={(e) => void handleSubmit(e)} className="grid gap-4">
      <DialogHeader>
        <DialogTitle>Editar {host.hostname}</DialogTitle>
        <DialogDescription>Cambia el grupo del host.</DialogDescription>
      </DialogHeader>
      <div className="grid gap-1.5">
        <label htmlFor="edit-group" className="text-sm font-medium text-text">
          Grupo
        </label>
        <GroupSelect id="edit-group" value={groupId} onChange={setGroupId} groups={groups?.items ?? []} />
      </div>
      {error && <InlineError>{error}</InlineError>}
      <DialogFooter>
        <Button type="button" variant="ghost" onClick={onClose}>
          Cancelar
        </Button>
        <Button type="submit" disabled={submitting}>
          {submitting ? 'Guardando...' : 'Guardar'}
        </Button>
      </DialogFooter>
    </form>
  )
}
