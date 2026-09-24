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
import { bulkHosts } from '../services/hosts.service'
import type { BulkRequest } from '../types/host.types'
import { TAG_ERRORS } from '../utils/tags'
import { TagsInput } from './TagsInput'

export type BulkMode = 'set_group' | 'add_tag' | 'remove_tag'

interface BulkDialogProps {
  mode: BulkMode | null
  ids: string[]
  onClose: () => void
  onDone: () => void
}

const TITLES: Record<BulkMode, string> = {
  set_group: 'Asignar grupo',
  add_tag: 'Agregar etiqueta',
  remove_tag: 'Quitar etiqueta',
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
  const [tags, setTags] = useState<string[]>([])
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const { groups } = useGroups()

  function buildRequest(): BulkRequest | string {
    if (mode === 'set_group') return { ids, action: 'set_group', group_id: groupId }
    const tag = tags[0]
    if (!tag) return 'Escribe una etiqueta.'
    return { ids, action: mode, tag }
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setError(null)
    const request = buildRequest()
    if (typeof request === 'string') return setError(request)
    setSubmitting(true)
    try {
      await bulkHosts(request)
      onDone()
    } catch (err) {
      const code = err instanceof ApiError ? err.code : ''
      setError(
        code === 'too_many_tags'
          ? 'Algún host superaría el máximo de 20 etiquetas.'
          : code === 'invalid_tag'
            ? TAG_ERRORS.invalid_tag
            : code === 'unknown_hosts'
            ? 'Algún host ya no existe. Actualiza la lista.'
            : code === 'group_not_found'
              ? 'El grupo ya no existe.'
              : err instanceof Error
                ? err.message
                : 'Error desconocido',
      )
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <form onSubmit={(e) => void handleSubmit(e)} className="grid gap-4">
      <DialogHeader>
        <DialogTitle>{TITLES[mode]}</DialogTitle>
        <DialogDescription>
          Se aplica a {ids.length} {ids.length === 1 ? 'host seleccionado' : 'hosts seleccionados'}.
        </DialogDescription>
      </DialogHeader>
      {mode === 'set_group' ? (
        <div className="grid gap-1.5">
          <label htmlFor="bulk-group" className="text-sm font-medium text-text">
            Grupo
          </label>
          <GroupSelect id="bulk-group" value={groupId} onChange={setGroupId} groups={groups?.items ?? []} />
        </div>
      ) : (
        <TagsInput id="bulk-tag" label="Etiqueta" value={tags} onChange={setTags} max={1} autoFocus />
      )}
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
