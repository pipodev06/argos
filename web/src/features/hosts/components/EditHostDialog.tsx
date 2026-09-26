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
import { FormField } from '@/components/ui/form-field'
import { InlineError } from '@/components/ui/inline-error'
import { GroupSelect } from '@/features/groups/components/GroupSelect'
import { useGroups } from '@/features/groups/hooks/useGroups'
import { mapCollectorRuleError } from '@/features/devices/utils/collectorRule'
import { RetentionField } from '@/features/retention/components/RetentionField'
import { ShrinkConfirmDialog } from '@/features/retention/components/ShrinkConfirmDialog'
import { putHostRetention } from '@/features/retention/services/retention.service'
import type { ShrinkDetails } from '@/features/retention/types/retention.types'
import { parseDetailDays, parseShrink } from '@/features/retention/utils/retention'
import { ApiError } from '@/lib/api'
import { renameHost, setHostGroup } from '../services/hosts.service'
import type { GroupRef, HostDetail } from '../types/host.types'

interface EditHostDialogProps {
  host: (Pick<HostDetail, 'id' | 'hostname'> & { group: GroupRef | null } & Partial<Pick<HostDetail, 'detail_retention_days' | 'effective_detail_days'>>) | null
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
  const [name, setName] = useState(host.hostname)
  const [groupId, setGroupId] = useState<string | null>(host.group?.id ?? null)
  const [retRaw, setRetRaw] = useState(String(host.detail_retention_days ?? host.effective_detail_days ?? ''))
  const [retInherit, setRetInherit] = useState(host.detail_retention_days == null)
  const [retError, setRetError] = useState<string | null>(null)
  const [shrink, setShrink] = useState<ShrinkDetails | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const { groups } = useGroups()

  // Solo se edita la retencion si el detalle la trae (host con agente).
  const hasRetention = host.effective_detail_days !== undefined

  async function submit(confirmShrink: boolean) {
    const newName = name.trim()
    if (!newName) return setError('El nombre no puede estar vacío.')
    setError(null)
    setRetError(null)
    let retDays: number | null | undefined
    if (hasRetention) {
      const parsed = retInherit ? null : parseDetailDays(retRaw)
      if (parsed && 'error' in parsed) return setRetError(parsed.error)
      const next = parsed ? parsed.days : null
      if (next !== (host.detail_retention_days ?? null)) retDays = next
    }
    setSubmitting(true)
    try {
      // Primero la retencion: si pide confirmar (409) todavia no se cambio nada.
      if (retDays !== undefined) await putHostRetention(host.id, retDays, confirmShrink)
      if (newName !== host.hostname) await renameHost(host.id, newName)
      if (groupId !== (host.group?.id ?? null)) await setHostGroup(host.id, groupId)
      setShrink(null)
      onSaved()
    } catch (err) {
      const s = parseShrink(err)
      if (s) return setShrink(s)
      const code = err instanceof ApiError ? err.code : ''
      setError(ERRORS[code] ?? (err instanceof ApiError ? mapCollectorRuleError(err) : null) ?? (err instanceof Error ? err.message : 'Error desconocido'))
    } finally {
      setSubmitting(false)
    }
  }

  function handleSubmit(e: FormEvent) {
    e.preventDefault()
    void submit(false)
  }

  return (
    <form onSubmit={handleSubmit} className="grid gap-4">
      <DialogHeader>
        <DialogTitle>Editar {host.hostname}</DialogTitle>
        <DialogDescription>Cambia el nombre o el grupo del host.</DialogDescription>
      </DialogHeader>
      <FormField
        id="edit-name"
        label="Nombre"
        value={name}
        onChange={(e) => setName(e.target.value)}
        maxLength={253}
        autoComplete="off"
        autoFocus
      />
      <div className="grid gap-1.5">
        <label htmlFor="edit-group" className="text-sm font-medium text-text">
          Grupo
        </label>
        <GroupSelect id="edit-group" value={groupId} onChange={setGroupId} groups={groups?.items ?? []} />
      </div>
      {hasRetention && (
        <RetentionField
          id="edit-retention"
          value={retRaw}
          inherit={retInherit}
          error={retError}
          inheritsFrom="la del grupo o, si no tiene, la global"
          onValue={setRetRaw}
          onInherit={setRetInherit}
        />
      )}
      {error && <InlineError>{error}</InlineError>}
      <DialogFooter>
        <Button type="button" variant="ghost" onClick={onClose}>
          Cancelar
        </Button>
        <Button type="submit" disabled={submitting}>
          {submitting ? 'Guardando...' : 'Guardar'}
        </Button>
      </DialogFooter>
      <ShrinkConfirmDialog
        shrink={shrink}
        busy={submitting}
        error={error}
        onConfirm={() => void submit(true)}
        onClose={() => setShrink(null)}
      />
    </form>
  )
}
