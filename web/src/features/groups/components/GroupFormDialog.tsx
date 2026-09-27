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
import { Input } from '@/components/ui/input'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { ApiError } from '@/lib/api'
import { RetentionField } from '@/features/retention/components/RetentionField'
import { ShrinkConfirmDialog } from '@/features/retention/components/ShrinkConfirmDialog'
import type { ShrinkDetails } from '@/features/retention/types/retention.types'
import { parseDetailDays, parseShrink } from '@/features/retention/utils/retention'
import { createGroup, updateGroup } from '../services/groups.service'
import { useGroupUsage } from '../hooks/useGroupUsage'
import type { Group, GroupInput, GroupQuotas, GroupUsageItem } from '../types/group.types'
import { parseThreshold, splitThreshold, type ThresholdUnit } from '../utils/format'

const QUOTA_FIELDS: readonly { key: keyof GroupQuotas; input: keyof GroupInput; label: string }[] = [
  { key: 'max_hosts', input: 'max_hosts', label: 'Hosts' },
  { key: 'max_devices', input: 'max_devices', label: 'Dispositivos' },
  { key: 'max_checks', input: 'max_checks', label: 'Chequeos' },
  { key: 'max_users', input: 'max_users', label: 'Usuarios' },
  { key: 'max_rules', input: 'max_rules', label: 'Reglas de alerta' },
  { key: 'max_channels', input: 'max_channels', label: 'Canales' },
]

/** "3 / 10" si hay cuota; "3 en uso" sin límite; nada si aún no se sabe el consumo. */
function usageHint(usage: GroupUsageItem | undefined): string | undefined {
  if (!usage) return undefined
  return usage.limit === null ? `${usage.current} en uso, sin límite` : `${usage.current} de ${usage.limit} en uso`
}

interface GroupFormDialogProps {
  /** null = crear; un grupo = editar. */
  group: Group | null
  open: boolean
  defaultDownAfter: number
  onClose: () => void
  onSaved: () => void
}

const MAX_NAME = 64

export function GroupFormDialog(props: GroupFormDialogProps) {
  // El formulario se monta solo con el dialogo abierto: cada apertura empieza con el estado del grupo.
  return (
    <Dialog open={props.open} onOpenChange={(next) => !next && props.onClose()}>
      <DialogContent>{props.open && <GroupForm {...props} />}</DialogContent>
    </Dialog>
  )
}

function GroupForm({ group, defaultDownAfter, onClose, onSaved }: GroupFormDialogProps) {
  const initial = group?.down_after_seconds != null ? splitThreshold(group.down_after_seconds) : null
  const [name, setName] = useState(group?.name ?? '')
  const [amount, setAmount] = useState(initial?.amount ?? '')
  const [unit, setUnit] = useState<ThresholdUnit>(initial?.unit ?? 'min')
  const [alwaysOn, setAlwaysOn] = useState(group?.always_on ?? false)
  const [nameError, setNameError] = useState<string | null>(null)
  const [amountError, setAmountError] = useState<string | null>(null)
  const [retRaw, setRetRaw] = useState(String(group?.detail_retention_days ?? group?.effective_detail_days ?? ''))
  const [retInherit, setRetInherit] = useState(group?.detail_retention_days == null)
  const [retError, setRetError] = useState<string | null>(null)
  const [shrink, setShrink] = useState<ShrinkDetails | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [quotaRaw, setQuotaRaw] = useState<Record<keyof GroupQuotas, string>>(() => {
    const q = group?.quotas
    const raw = { max_hosts: '', max_devices: '', max_checks: '', max_users: '', max_rules: '', max_channels: '' }
    if (q) for (const k of Object.keys(raw) as (keyof GroupQuotas)[]) raw[k] = q[k] == null ? '' : String(q[k])
    return raw
  })
  const [quotaError, setQuotaError] = useState<string | null>(null)
  const usage = useGroupUsage(group?.id ?? null)
  const usageByField: Record<keyof GroupQuotas, GroupUsageItem | undefined> = usage.data
    ? {
        max_hosts: usage.data.hosts, max_devices: usage.data.devices, max_checks: usage.data.checks,
        max_users: usage.data.users, max_rules: usage.data.rules, max_channels: usage.data.channels,
      }
    : { max_hosts: undefined, max_devices: undefined, max_checks: undefined, max_users: undefined, max_rules: undefined, max_channels: undefined }

  async function submit(confirmShrink: boolean) {
    const trimmed = name.trim()
    setNameError(null)
    setAmountError(null)
    setRetError(null)
    setQuotaError(null)
    setError(null)
    if (!trimmed) return setNameError('Ingresa un nombre.')
    if (trimmed.length > MAX_NAME) return setNameError(`Máximo ${MAX_NAME} caracteres.`)
    let downAfter: number | null = null
    if (amount.trim()) {
      const parsed = parseThreshold(amount, unit)
      if ('error' in parsed) return setAmountError(parsed.error)
      downAfter = parsed.seconds
    }
    const input: GroupInput = { name: trimmed, down_after_seconds: downAfter, always_on: alwaysOn }
    const days = retInherit ? null : parseDetailDays(retRaw)
    if (days && 'error' in days) return setRetError(days.error)
    const next = days ? days.days : null
    if (!group) {
      // Un grupo nuevo no tiene detalle que perder: no hay confirmación de reducción.
      if (next !== null) input.detail_retention_days = next
    } else if (next !== group.detail_retention_days) {
      input.detail_retention_days = next
      input.confirm_shrink = confirmShrink
    }
    if (group) {
      // Siempre se mandan los 6 (vacio = sin limite): el campo ya arranca con el valor actual, asi que no
      // tocarlo lo deja igual; borrarlo lo quita.
      const quotas: Partial<Record<keyof GroupInput, number | null>> = {}
      for (const { key, input: field, label } of QUOTA_FIELDS) {
        const raw = quotaRaw[key].trim()
        if (raw === '') {
          quotas[field] = null
          continue
        }
        const n = Number(raw)
        if (!Number.isInteger(n) || n < 0) return setQuotaError(`${label}: ingresa un entero >= 0 (vacío = sin límite).`)
        quotas[field] = n
      }
      Object.assign(input, quotas)
    }
    setSubmitting(true)
    try {
      if (group) await updateGroup(group.id, input)
      else await createGroup(input)
      onSaved()
    } catch (err) {
      const s = parseShrink(err)
      if (s) return setShrink(s)
      const code = err instanceof ApiError ? err.code : ''
      if (code === 'group_exists') setNameError('Ya existe un grupo con ese nombre.')
      else if (code === 'too_many_groups') setError('Se alcanzó el máximo de 200 grupos.')
      else setError(err instanceof Error ? err.message : 'Error desconocido')
    } finally {
      setSubmitting(false)
    }
  }

  function handleSubmit(e: FormEvent) {
    e.preventDefault()
    void submit(false)
  }

  const globalMin = Math.round(defaultDownAfter / 60)

  return (
    <form onSubmit={handleSubmit} className="grid gap-4">
      <DialogHeader>
        <DialogTitle>{group ? 'Editar grupo' : 'Nuevo grupo'}</DialogTitle>
        <DialogDescription>Define cuándo un host del grupo se considera sin reportar.</DialogDescription>
      </DialogHeader>
      <FormField
        id="group-name"
        label="Nombre"
        value={name}
        onChange={(e) => setName(e.target.value)}
        maxLength={MAX_NAME}
        autoComplete="off"
        autoFocus
        error={nameError}
      />
      <div className="grid gap-1.5">
        <label htmlFor="group-threshold" className="text-sm font-medium text-text">
          Considerar sin reportar tras
        </label>
        <div className="flex items-center gap-2">
          <Input
            id="group-threshold"
            inputMode="decimal"
            className="w-24"
            value={amount}
            onChange={(e) => setAmount(e.target.value)}
            placeholder={String(globalMin)}
            aria-invalid={amountError ? true : undefined}
            aria-describedby="group-threshold-hint"
          />
          <Select value={unit} onValueChange={(v) => setUnit(v as ThresholdUnit)}>
            <SelectTrigger aria-label="Unidad" className="w-28">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="min">minutos</SelectItem>
              <SelectItem value="h">horas</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <p id="group-threshold-hint" className="text-xs text-text-muted">
          Vacío = usa el valor global ({globalMin} min).
        </p>
        {amountError && <InlineError>{amountError}</InlineError>}
      </div>
      <fieldset className="grid gap-2">
        <legend className="mb-1 text-sm font-medium text-text">Al superar el umbral mostrar como</legend>
        <RadioGroup
          value={alwaysOn ? 'down' : 'offline'}
          onValueChange={(v) => setAlwaysOn(v === 'down')}
          aria-label="Al superar el umbral mostrar como"
        >
          <label htmlFor="group-down" className="flex min-h-8 items-start gap-2 text-sm">
            <RadioGroupItem id="group-down" value="down" className="mt-0.5" />
            <span>
              Caído
              <span className="block text-xs text-text-muted">Se espera que esté siempre encendido.</span>
            </span>
          </label>
          <label htmlFor="group-offline" className="flex min-h-8 items-start gap-2 text-sm">
            <RadioGroupItem id="group-offline" value="offline" className="mt-0.5" />
            <span>
              Sin conexión
              <span className="block text-xs text-text-muted">Apagarlo o perder red es normal.</span>
            </span>
          </label>
        </RadioGroup>
      </fieldset>
      <RetentionField
        id="group-retention"
        value={retRaw}
        inherit={retInherit}
        error={retError}
        inheritsFrom="la retención global"
        onValue={setRetRaw}
        onInherit={setRetInherit}
      />
      {group && (
        <fieldset className="grid gap-2">
          <legend className="mb-1 text-sm font-medium text-text">Cuotas del cliente</legend>
          <p className="text-xs text-text-muted">Vacío = sin límite propio. No afecta lo que ya existe, solo lo nuevo.</p>
          <div className="grid grid-cols-2 gap-3">
            {QUOTA_FIELDS.map(({ key, label }) => (
              <FormField
                key={key}
                id={`group-${key}`}
                label={label}
                inputMode="numeric"
                value={quotaRaw[key]}
                onChange={(e) => setQuotaRaw((prev) => ({ ...prev, [key]: e.target.value }))}
                hint={usageHint(usageByField[key])}
              />
            ))}
          </div>
          {quotaError && <InlineError>{quotaError}</InlineError>}
        </fieldset>
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
