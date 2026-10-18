import { useState, type FormEvent } from 'react'
import { Button } from '@/components/ui/button'
import { CheckField } from '@/components/ui/check-field'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { FormField } from '@/components/ui/form-field'
import { FormHelp, type HelpItem } from '@/components/ui/form-help'
import { GroupSelect } from '@/features/groups/components/GroupSelect'
import { useGroups } from '@/features/groups/hooks/useGroups'
import { EnrollTokenStep } from './EnrollTokenStep'
import { useEnrollHost } from '../hooks/useEnrollHost'
import { MAX_BULK_ENROLL_USES, MIN_BULK_ENROLL_USES, parseBulkUses } from '../utils/enroll'

const ADD_HOST_HELP: readonly HelpItem[] = [
  ['Hostname', 'Nombre con el que aparecerá el host. El token generado sirve una sola vez y vence a los 15 minutos.'],
  ['Alta masiva', 'Un solo token para varios equipos: cada uno se registra con su propio hostname hasta agotar los usos.'],
  ['Grupo', 'Grupo al que entra el host. Obligatorio en alta masiva.'],
  ['Cuántos equipos', `Usos del token en alta masiva: entre ${MIN_BULK_ENROLL_USES} y ${MAX_BULK_ENROLL_USES}.`],
]

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
  const [bulk, setBulk] = useState(false)
  const [bulkUsesRaw, setBulkUsesRaw] = useState('50')
  const [bulkError, setBulkError] = useState<string | null>(null)
  const { groups } = useGroups()
  const { token, error, submitting, submit, reset } = useEnrollHost()

  function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setBulkError(null)
    if (bulk) {
      if (!groupId) return setBulkError('El alta masiva necesita un grupo.')
      const parsed = parseBulkUses(bulkUsesRaw)
      if (!parsed.ok) return setBulkError(parsed.error)
      void submit({ hostname: '', groupId, maxUses: parsed.uses })
      return
    }
    const trimmed = hostname.trim()
    if (trimmed) void submit({ hostname: trimmed, groupId })
  }

  if (token) return <EnrollTokenStep token={token} title="Instala el agente" onClose={onClose} onAnother={reset} />

  return (
    <form onSubmit={handleSubmit} className="grid gap-3">
      <DialogHeader className="flex-row items-center gap-1">
        <DialogTitle>Agregar host</DialogTitle>
        <FormHelp title="Agregar host" items={ADD_HOST_HELP} />
      </DialogHeader>
      <CheckField
        id="enroll-bulk"
        label="Alta masiva"
        checked={bulk}
        onChange={setBulk}
      />
      {!bulk && (
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
      )}
      <div className="grid gap-1">
        <label htmlFor="enroll-group" className="text-sm font-medium text-text">
          Grupo
        </label>
        <GroupSelect id="enroll-group" value={groupId} onChange={setGroupId} groups={groups?.items ?? []} />
      </div>
      {bulk && (
        <FormField
          id="enroll-bulk-uses"
          label="Cuántos equipos"
          inputMode="numeric"
          className="w-28"
          value={bulkUsesRaw}
          onChange={(e) => setBulkUsesRaw(e.target.value)}
          error={bulkError ?? error}
        />
      )}
      <DialogFooter>
        <Button type="button" variant="ghost" onClick={onClose}>
          Cancelar
        </Button>
        <Button type="submit" disabled={submitting || (bulk ? !groupId : !hostname.trim())}>
          {submitting ? 'Generando...' : 'Generar token'}
        </Button>
      </DialogFooter>
    </form>
  )
}
