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
import { mapCollectorRuleError } from '@/features/devices/utils/collectorRule'
import { bulkHosts } from '../services/hosts.service'
import type { BulkRequest } from '../types/host.types'

export type BulkMode = 'set_group'

interface BulkDialogProps {
  mode: BulkMode | null
  ids: string[]
  onClose: () => void
  onDone: () => void
}

const TITLES: Record<BulkMode, string> = {
  set_group: 'Asignar grupo',
}

export function BulkDialog({ mode, ids, onClose, onDone }: BulkDialogProps) {
  return (
    <Dialog open={mode !== null} onOpenChange={(next) => !next && onClose()}>
      <DialogContent>{mode && <BulkForm mode={mode} ids={ids} onClose={onClose} onDone={onDone} />}</DialogContent>
    </Dialog>
  )
}

function BulkForm({ mode, ids, onClose, onDone }: Omit<BulkDialogProps, 'mode'> & { mode: BulkMode }) {
  const [groupId, setGroupId] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const { groups } = useGroups()

  function buildRequest(): BulkRequest {
    return { ids, action: mode, group_id: groupId }
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setError(null)
    const request = buildRequest()
    setSubmitting(true)
    try {
      await bulkHosts(request)
      onDone()
    } catch (err) {
      const code = err instanceof ApiError ? err.code : ''
      setError(
        code === 'unknown_hosts'
          ? 'Algún host ya no existe. Actualiza la lista.'
          : code === 'group_not_found'
            ? 'El grupo ya no existe.'
            : (err instanceof ApiError ? mapCollectorRuleError(err) : null) ??
              (err instanceof Error ? err.message : 'Error desconocido'),
      )
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <form onSubmit={(e) => void handleSubmit(e)} className="grid gap-3">
      <DialogHeader>
        <DialogTitle>{TITLES[mode]}</DialogTitle>
        <DialogDescription>
          Se aplica a {ids.length} {ids.length === 1 ? 'host seleccionado' : 'hosts seleccionados'}.
        </DialogDescription>
      </DialogHeader>
      <div className="grid gap-1.5">
        <label htmlFor="bulk-group" className="text-sm font-medium text-text">
          Grupo
        </label>
        <GroupSelect id="bulk-group" value={groupId} onChange={setGroupId} groups={groups?.items ?? []} />
      </div>
      {error && <InlineError>{error}</InlineError>}
      <DialogFooter>
        <Button type="button" variant="ghost" onClick={onClose}>
          Cancelar
        </Button>
        <Button type="submit" disabled={submitting}>
          {submitting ? 'Aplicando...' : 'Aplicar'}
        </Button>
      </DialogFooter>
    </form>
  )
}
