import { useState, type FormEvent } from 'react'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { FormField } from '@/components/ui/form-field'
import { FormHelp, type HelpItem } from '@/components/ui/form-help'
import { InlineError } from '@/components/ui/inline-error'
import { SelectField } from '@/components/ui/select-field'
import type { AlertSeverity } from '@/features/alerts/types/alert.types'
import { SEVERITY_LABELS } from '@/features/alerts/utils/format'
import { useGroups } from '@/features/groups/hooks/useGroups'
import { useUserMutation } from '@/features/users/hooks/useUserMutation'
import { createChannel, updateChannel } from '../services/channels.service'
import type { Channel, UpdateChannelInput } from '../types/channel.types'
import { mapChannelError } from '../utils/errors'
import { GLOBAL_OWNER, ownerToGroupId, ownerValue } from '../utils/owner'

interface ChannelFormDialogProps {
  /** null = crear; un canal = editar. */
  channel: Channel | null
  open: boolean
  onClose: () => void
  onSaved: () => void
}

const MAX_NAME = 80
const SEVERITY_OPTIONS = (Object.keys(SEVERITY_LABELS) as AlertSeverity[]).map((s) => ({ value: s, label: SEVERITY_LABELS[s] }))

const CHANNEL_HELP: readonly HelpItem[] = [
  ['URL del webhook', 'Solo https y puerto 443; no se aceptan destinos internos. Se guarda cifrada y no se vuelve a mostrar.'],
  ['Secreto HMAC', 'Opcional. Firma cada envío con HMAC-SHA256 para que el receptor verifique el origen. Al editar, vacío conserva el guardado.'],
  ['Dueño', 'Un canal de un cliente solo se usa en reglas de ese cliente; uno Global sirve para cualquier regla.'],
  ['Severidad mínima', 'Solo avisa alertas de esta severidad o mayor.'],
  ['Canal activo', 'Desactivado no envía y descarta las entregas pendientes.'],
]

export function ChannelFormDialog(props: ChannelFormDialogProps) {
  // El formulario solo existe con el diálogo abierto: al cerrar, la URL y el secreto se descartan.
  return (
    <Dialog open={props.open} onOpenChange={(next) => !next && props.onClose()}>
      <DialogContent className="max-h-[90dvh] overflow-y-auto">{props.open && <ChannelForm {...props} />}</DialogContent>
    </Dialog>
  )
}

function ChannelForm({ channel, onClose, onSaved }: ChannelFormDialogProps) {
  const editing = channel !== null
  // URL y secreto viven solo en este estado local; el valor guardado nunca llega al cliente.
  const [name, setName] = useState(channel?.name ?? '')
  const [url, setUrl] = useState('')
  const [secret, setSecret] = useState('')
  const [removeSecret, setRemoveSecret] = useState(false)
  const [severity, setSeverity] = useState<AlertSeverity>(channel?.min_severity ?? 'warn')
  const [enabled, setEnabled] = useState(channel?.enabled ?? true)
  const [owner, setOwner] = useState(() => ownerValue(channel))
  const { groups } = useGroups()
  const ownerOptions = [{ value: GLOBAL_OWNER, label: 'Global' }, ...(groups?.items ?? []).map((g) => ({ value: g.id, label: g.name }))]
  const [errors, setErrors] = useState<{ name?: string; url?: string }>({})
  const { error, busy, run } = useUserMutation()

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    const errs: { name?: string; url?: string } = {}
    const trimmed = name.trim()
    if (!trimmed) errs.name = 'Ingresa un nombre.'
    else if (trimmed.length > MAX_NAME) errs.name = `Máximo ${MAX_NAME} caracteres.`
    const u = url.trim()
    if (!editing && !u) errs.url = 'Ingresa la URL del webhook.'
    else if (u && !/^https:\/\//i.test(u)) errs.url = 'La URL debe empezar con https://'
    setErrors(errs)
    if (errs.name || errs.url) return

    const ok = await run(() => {
      if (!channel) {
        return createChannel({
          name: trimmed,
          kind: 'webhook',
          url: u,
          ...(secret && { hmac_secret: secret }),
          min_severity: severity,
          enabled,
          group_id: ownerToGroupId(owner),
        })
      }
      const patch: UpdateChannelInput = { name: trimmed, min_severity: severity, enabled }
      if (u) patch.url = u
      if (owner !== ownerValue(channel)) patch.group_id = ownerToGroupId(owner)
      if (removeSecret) patch.hmac_secret = null
      else if (secret) patch.hmac_secret = secret
      return updateChannel(channel.id, patch)
    }, mapChannelError)
    if (ok) onSaved()
  }

  const hasSecret = editing && channel.has_secret

  return (
    <form onSubmit={(e) => void handleSubmit(e)} className="grid gap-3" noValidate autoComplete="off">
      <DialogHeader className="flex-row items-center gap-1">
        <DialogTitle>{editing ? 'Editar canal' : 'Nuevo canal'}</DialogTitle>
        <FormHelp title="Canal" items={CHANNEL_HELP} />
      </DialogHeader>
      <FormField id="channel-name" label="Nombre" value={name} onChange={(e) => setName(e.target.value)} maxLength={MAX_NAME} autoFocus error={errors.name} />
      <FormField
        id="channel-url"
        label={editing ? 'Reemplazar URL del webhook' : 'URL del webhook'}
        type="url"
        value={url}
        onChange={(e) => setUrl(e.target.value)}
        placeholder="https://"
        autoComplete="off"
        spellCheck={false}
        error={errors.url}
      />
      <FormField
        id="channel-secret"
        label={hasSecret ? 'Reemplazar secreto HMAC' : 'Secreto HMAC'}
        type="password"
        value={secret}
        onChange={(e) => setSecret(e.target.value)}
        disabled={removeSecret}
        autoComplete="new-password"
        placeholder={hasSecret ? 'Sin cambios' : undefined}
      />
      {hasSecret && (
        <label htmlFor="channel-remove-secret" className="flex items-center gap-2 text-sm">
          <Checkbox id="channel-remove-secret" checked={removeSecret} onCheckedChange={(v) => setRemoveSecret(v === true)} />
          Quitar firma
        </label>
      )}
      <SelectField
        id="channel-owner"
        label="Dueño"
        value={owner}
        onChange={setOwner}
        options={ownerOptions}
      />
      <SelectField id="channel-severity" label="Severidad mínima" value={severity} onChange={(v) => setSeverity(v as AlertSeverity)} options={SEVERITY_OPTIONS} />
      <label htmlFor="channel-enabled" className="flex items-start gap-2 text-sm">
        <Checkbox id="channel-enabled" className="mt-0.5" checked={enabled} onCheckedChange={(v) => setEnabled(v === true)} />
        <span>Canal activo</span>
      </label>
      {error && <InlineError>{error}</InlineError>}
      <DialogFooter>
        <Button type="button" variant="ghost" onClick={onClose}>
          Cancelar
        </Button>
        <Button type="submit" disabled={busy}>
          {busy ? 'Guardando...' : 'Guardar'}
        </Button>
      </DialogFooter>
    </form>
  )
}
