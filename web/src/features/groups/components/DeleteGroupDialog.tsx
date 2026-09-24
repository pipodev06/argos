import { useState } from 'react'
import { IconAlertTriangle } from '@tabler/icons-react'
import { Link } from 'react-router'
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
  const blocked = group !== null && group.host_count > 0

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
          ? 'El grupo ya tiene hosts: reasígnalos antes de eliminar.'
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
            <IconAlertTriangle className="size-5 shrink-0 text-danger" stroke={1.5} aria-hidden />
            {blocked ? 'No se puede eliminar' : `Eliminar "${group?.name ?? ''}"`}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {blocked ? (
              <>
                Reasigna los {group.host_count} hosts antes de eliminar.{' '}
                <Link to={`/hosts?group=${group.id}`} className="underline" onClick={onClose}>
                  Ver hosts del grupo
                </Link>
              </>
            ) : (
              'El grupo se elimina de forma definitiva.'
            )}
          </AlertDialogDescription>
        </AlertDialogHeader>
        {error && <InlineError>{error}</InlineError>}
        <AlertDialogFooter>
          <AlertDialogCancel>{blocked ? 'Cerrar' : 'Cancelar'}</AlertDialogCancel>
          {!blocked && (
            <AlertDialogAction
              disabled={busy}
              onClick={(e) => {
                e.preventDefault()
                void handleDelete()
              }}
            >
              {busy ? 'Eliminando...' : 'Eliminar'}
            </AlertDialogAction>
          )}
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
