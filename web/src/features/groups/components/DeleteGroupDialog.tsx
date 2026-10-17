import { useState } from 'react'
import { TriangleAlertIcon } from 'lucide-react'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { InlineError } from '@/components/ui/inline-error'
import { ApiError } from '@/lib/api'
import { deleteGroup } from '../services/groups.service'
import type { Group } from '../types/group.types'

interface DeleteGroupDialogProps {
  group: Group | null
  onClose: () => void
  onDeleted: () => void
}

export function DeleteGroupDialog({ group, onClose, onDeleted }: DeleteGroupDialogProps) {
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  function handleOpenChange(next: boolean) {
    if (next) return
    setError(null)
    onClose()
  }

  async function handleDelete() {
    if (!group) return
    setBusy(true)
    setError(null)
    try {
      await deleteGroup(group.id)
      onDeleted()
    } catch (err) {
      setError(
        err instanceof ApiError && err.code === 'group_not_empty'
          ? 'El grupo ya tiene hosts: recarga la página.'
          : err instanceof Error
            ? err.message
            : 'Error desconocido',
      )
    } finally {
      setBusy(false)
    }
  }

  return (
    <AlertDialog open={group !== null} onOpenChange={handleOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle className="flex items-center gap-2">
            <TriangleAlertIcon className="size-5 shrink-0 text-danger" strokeWidth={1.5} aria-hidden />
            {`Eliminar "${group?.name ?? ''}"`}
          </AlertDialogTitle>
          <AlertDialogDescription>
            El grupo está vacío y se elimina de forma definitiva.
          </AlertDialogDescription>
        </AlertDialogHeader>
        {error && <InlineError>{error}</InlineError>}
        <AlertDialogFooter>
          <AlertDialogCancel>Cancelar</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            disabled={busy}
            onClick={(e) => {
              e.preventDefault()
              void handleDelete()
            }}
          >
            {busy ? 'Eliminando...' : 'Eliminar'}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
