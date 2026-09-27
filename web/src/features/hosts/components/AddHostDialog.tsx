import { useState, type FormEvent } from 'react'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { FormField } from '@/components/ui/form-field'
import { GroupSelect } from '@/features/groups/components/GroupSelect'
import { useGroups } from '@/features/groups/hooks/useGroups'
import { EnrollTokenStep } from './EnrollTokenStep'
import { useEnrollHost } from '../hooks/useEnrollHost'

interface AddHostDialogProps {
  open: boolean
  onClose: () => void
}

export function AddHostDialog({ open, onClose }: AddHostDialogProps) {
  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      {/* El contenido se monta solo abierto: al cerrar se descartan hostname, token y comandos del estado. */}
      <DialogContent className="sm:max-w-2xl">{open && <AddHostBody onClose={onClose} />}</DialogContent>
    </Dialog>
  )
}

function AddHostBody({ onClose }: { onClose: () => void }) {
  const [hostname, setHostname] = useState('')
  const [groupId, setGroupId] = useState<string | null>(null)
  const { groups } = useGroups()
  const { token, error, submitting, submit, reset } = useEnrollHost()

  function handleSubmit(e: FormEvent) {
    e.preventDefault()
    const trimmed = hostname.trim()
    if (trimmed) void submit(trimmed, groupId)
  }

  if (token) return <EnrollTokenStep token={token} title="Instala el agente" onClose={onClose} onAnother={reset} />

  return (
    <form onSubmit={handleSubmit} className="grid gap-4">
      <DialogHeader>
        <DialogTitle>Agregar host</DialogTitle>
        <DialogDescription>Genera un token para enrolar el agente en este host.</DialogDescription>
      </DialogHeader>
      <FormField
        id="hostname"
        label="Hostname"
        value={hostname}
        onChange={(e) => setHostname(e.target.value)}
        placeholder="srv-web-01"
        autoComplete="off"
        autoFocus
        error={error}
      />
      <div className="grid gap-1.5">
        <label htmlFor="enroll-group" className="text-sm font-medium text-text">
          Grupo
        </label>
        <GroupSelect id="enroll-group" value={groupId} onChange={setGroupId} groups={groups?.items ?? []} />
      </div>
      <DialogFooter>
        <Button type="button" variant="ghost" onClick={onClose}>
          Cancelar
        </Button>
        <Button type="submit" disabled={submitting || !hostname.trim()}>
          {submitting ? 'Generando...' : 'Generar token'}
        </Button>
      </DialogFooter>
    </form>
  )
}
