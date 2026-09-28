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
import { purgeGroup } from '../services/purge.service'
import type { GroupPurgeResult } from '../types/purge.types'
import { downloadBlob } from '@/lib/download'
import { buildConstancia, constanciaFilename, mapPurgeGroupError, purgeNameMatches, summarizeGroupPurge } from '../utils/purge'

interface PurgeGroupDialogProps {
  group: Group | null
  onClose: () => void
  /** Se llama al cerrar tras una baja hecha (aunque falle algun host) para refrescar la lista. */
  onDone: () => void
}

/** Baja de un cliente: purga todos los hosts y dispositivos del grupo. Larga e irreversible. */
export function PurgeGroupDialog({ group, onClose, onDone }: PurgeGroupDialogProps) {
  return group ? <PurgeGroupForm group={group} onClose={onClose} onDone={onDone} /> : null
}

function PurgeGroupForm({ group, onClose, onDone }: { group: Group } & Omit<PurgeGroupDialogProps, 'group'>) {
  const [typed, setTyped] = useState('')
  const [deleteGroup, setDeleteGroup] = useState(false)
  const [disableUsers, setDisableUsers] = useState(false)
  const [result, setResult] = useState<GroupPurgeResult | null>(null)
  const { error, busy, run } = useUserMutation()
  const exportMutation = useUserMutation()

  async function exportBackup() {
    await exportMutation.run(() => downloadGroupExport(group.id, group.name))
  }

  async function confirm() {
    // Sin reintentos automaticos: una sola llamada; si falla, el usuario decide repetirla (es idempotente).
    await run(async () => {
      setResult(await purgeGroup(group.id, typed, { delete_group: deleteGroup, disable_users: disableUsers }))
    }, mapPurgeGroupError)
  }

  function handleOpenChange(next: boolean) {
    if (next || busy) return
    if (result) onDone()
    else onClose()
  }

  const failed = result?.failed_host_ids.length ?? 0

  return (
    <AlertDialog open onOpenChange={handleOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle className="flex items-center gap-2">
            <IconAlertTriangle className="size-5 shrink-0 text-danger" stroke={1.5} aria-hidden />
            {result ? 'Baja completada' : `Dar de baja a "${group.name}"`}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {result
              ? failed > 0
                ? `${failed} ${failed === 1 ? 'host falló y no se purgó' : 'hosts fallaron y no se purgaron'}. Puedes repetir la baja: es seguro repetirla.`
                : 'Todos los datos del cliente se borraron.'
              : 'Se borran de forma definitiva todos los hosts y dispositivos del grupo con sus métricas, resumen horario, historial, discos y chequeos, además de sus reglas, ventanas de mantenimiento y tokens. No se borran los respaldos (se rotan a los 14 días) ni la auditoría. No se puede deshacer.'}
          </AlertDialogDescription>
        </AlertDialogHeader>
        {result ? (
          <ul className="grid gap-1 text-sm tabular-nums">
            {summarizeGroupPurge(result).map((l) => (
              <li key={l}>{l}</li>
            ))}
          </ul>
        ) : (
          <div className="grid gap-3">
            <div className="flex flex-col items-start gap-1.5">
              <Button type="button" variant="outline" size="sm" disabled={exportMutation.busy} onClick={() => void exportBackup()}>
                <IconDownload stroke={1.5} aria-hidden />
                {exportMutation.busy ? 'Exportando...' : 'Exportar antes de purgar'}
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
        )}
        {busy && (
          <p role="status" className="text-sm">
            Trabajando… puede tardar varios minutos. No cierres esta ventana.
          </p>
        )}
        {error && <InlineError>{error}</InlineError>}
        <AlertDialogFooter>
          {result ? (
            <>
              <Button
                type="button"
                variant="outline"
                onClick={() => downloadBlob(buildConstancia(result), constanciaFilename(result.group_name, result.purged_at))}
              >
                <IconDownload stroke={1.5} aria-hidden />
                Descargar constancia
              </Button>
              <AlertDialogAction onClick={onDone}>Cerrar</AlertDialogAction>
            </>
          ) : (
            <>
              <AlertDialogCancel disabled={busy}>Cancelar</AlertDialogCancel>
              <AlertDialogAction
                disabled={busy || !purgeNameMatches(typed, group.name)}
                onClick={(e) => {
                  e.preventDefault()
                  void confirm()
                }}
              >
                {busy ? 'Trabajando...' : 'Dar de baja'}
              </AlertDialogAction>
            </>
          )}
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
