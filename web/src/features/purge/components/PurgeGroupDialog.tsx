import { useState } from 'react'
import { IconAlertTriangle, IconDownload } from '@tabler/icons-react'
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
import { Button } from '@/components/ui/button'
import { CheckField } from '@/components/ui/check-field'
import { FormField } from '@/components/ui/form-field'
import { InlineError } from '@/components/ui/inline-error'
import { useUserMutation } from '@/features/users/hooks/useUserMutation'
import { downloadGroupExport } from '@/features/groups/services/groups.service'
import type { Group } from '@/features/groups/types/group.types'
import { usePurgeRun } from '../hooks/usePurgeRun'
import { purgeGroup } from '../services/purge.service'
import { downloadBlob } from '@/lib/download'
import { buildConstancia, constanciaFilename, groupResultOf, mapPurgeGroupError, purgeNameMatches, summarizeGroupPurge } from '../utils/purge'
import { PurgeJobDialog } from './PurgeJobDialog'

interface PurgeGroupDialogProps {
  group: Group | null
  onClose: () => void
  /** Al cerrar con la baja terminada (aunque falle algun host), o cuando termina si se cerro antes: refrescar la lista. */
  onDone: () => void
}

/** Baja de un cliente: purga todos los hosts y dispositivos del grupo. Corre en segundo plano; irreversible. */
export function PurgeGroupDialog({ group, onClose, onDone }: PurgeGroupDialogProps) {
  return group ? <PurgeGroupForm group={group} onClose={onClose} onDone={onDone} /> : null
}

function PurgeGroupForm({ group, onClose, onDone }: { group: Group } & Omit<PurgeGroupDialogProps, 'group'>) {
  const [typed, setTyped] = useState('')
  const [deleteGroup, setDeleteGroup] = useState(true)
  const [disableUsers, setDisableUsers] = useState(false)
  const exportMutation = useUserMutation()
  const { job, pollError, busy, error, start, close } = usePurgeRun(
    () => purgeGroup(group.id, typed, { delete_group: deleteGroup, disable_users: disableUsers }),
    mapPurgeGroupError,
    onDone,
    onClose,
  )

  async function exportBackup() {
    await exportMutation.run(() => downloadGroupExport(group.id, group.name))
  }

  if (job) {
    // La constancia solo existe con el trabajo terminado (`done`): en curso o fallido no hay resultado.
    const result = groupResultOf(job)
    const failedHosts = result?.failed_host_ids.length ?? 0
    return (
      <PurgeJobDialog
        job={job}
        pollError={pollError}
        retryError={error}
        retrying={busy}
        runningTitle={`Dando de baja a "${group.name}"`}
        doneTitle="Baja completada"
        doneContent={
          result && (
            <div className="grid gap-2">
              <p className="text-sm">
                {failedHosts > 0
                  ? `${failedHosts} ${failedHosts === 1 ? 'host falló y no se purgó' : 'hosts fallaron y no se purgaron'}. Puedes repetir la baja: es seguro repetirla.`
                  : 'Todos los datos del cliente se borraron.'}
              </p>
              <ul className="grid gap-1 text-sm tabular-nums">
                {summarizeGroupPurge(result).map((l) => (
                  <li key={l}>{l}</li>
                ))}
              </ul>
            </div>
          )
        }
        doneActions={
          result && (
            <Button
              type="button"
              variant="outline"
              onClick={() => downloadBlob(buildConstancia(result), constanciaFilename(result.group_name, result.purged_at))}
            >
              <IconDownload stroke={1.5} aria-hidden />
              Descargar constancia
            </Button>
          )
        }
        onRetry={() => void start()}
        onClose={close}
      />
    )
  }

  return (
    <AlertDialog open onOpenChange={(next) => !next && !busy && onClose()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle className="flex items-center gap-2">
            <IconAlertTriangle className="size-5 shrink-0 text-danger" stroke={1.5} aria-hidden />
            {`Eliminar "${group.name}" y sus ${group.host_count} hosts`}
          </AlertDialogTitle>
          <AlertDialogDescription>
            Se borran los hosts y dispositivos del grupo con todos sus datos. La auditoría se conserva. No se puede deshacer.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <div className="grid gap-3">
          <div className="flex flex-col items-start gap-1.5">
            <Button type="button" variant="outline" size="sm" disabled={exportMutation.busy} onClick={() => void exportBackup()}>
              <IconDownload stroke={1.5} aria-hidden />
              {exportMutation.busy ? 'Exportando...' : 'Exportar antes'}
            </Button>
            {exportMutation.error && <InlineError>{exportMutation.error}</InlineError>}
          </div>
          <FormField
            id="purge-group-name"
            label={`Escribe "${group.name}" para confirmar`}
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
            disabled={busy}
            autoComplete="off"
            autoFocus
          />
          <CheckField id="purge-group-delete" label="Eliminar también el grupo" checked={deleteGroup} onChange={setDeleteGroup} />
          <CheckField
            id="purge-group-users"
            label="Deshabilitar a los usuarios que solo tenían este grupo"
            checked={disableUsers}
            onChange={setDisableUsers}
          />
        </div>
        {error && <InlineError>{error}</InlineError>}
        <AlertDialogFooter>
          <AlertDialogCancel disabled={busy}>Cancelar</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            disabled={busy || !purgeNameMatches(typed, group.name)}
            onClick={(e) => {
              e.preventDefault()
              void start()
            }}
          >
            {busy ? 'Enviando...' : 'Eliminar'}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
