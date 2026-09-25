import { useState, type FormEvent } from 'react'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { FormField } from '@/components/ui/form-field'
import { InlineError } from '@/components/ui/inline-error'
import { SelectField } from '@/components/ui/select-field'
import { CheckSelectField } from '@/features/checks/components/CheckSelectField'
import { ChannelPicker } from '@/features/channels/components/ChannelPicker'
import { useUserMutation } from '@/features/users/hooks/useUserMutation'
import { createRule, setRuleChannels, updateRule } from '../services/alerts.service'
import type { AlertRule, AlertSeverity, RuleKind, RuleMetric, RuleOp } from '../types/alert.types'
import { isLoadMetric, isUpdatesKind, METRIC_LABELS, SEVERITY_LABELS } from '../utils/format'
import {
  buildRuleInput,
  KIND_OPTIONS,
  MAX_NAME,
  OP_OPTIONS,
  ruleToDraft,
  type RuleDraft,
  type RuleErrors,
} from '../utils/ruleForm'
import { ScopeFields } from './ScopeFields'
import { ALL_SCOPE, ruleScopeTypes } from '../utils/scope'

interface RuleFormDialogProps {
  /** null = crear; una regla = editar. */
  rule: AlertRule | null
  open: boolean
  onClose: () => void
  onSaved: () => void
}

const METRIC_OPTIONS = (Object.keys(METRIC_LABELS) as RuleMetric[]).map((m) => ({ value: m, label: METRIC_LABELS[m] }))
const SEVERITY_OPTIONS = (Object.keys(SEVERITY_LABELS) as AlertSeverity[]).map((s) => ({ value: s, label: SEVERITY_LABELS[s] }))

export function RuleFormDialog(props: RuleFormDialogProps) {
  // El formulario se monta solo con el diálogo abierto: cada apertura empieza con el estado de la regla.
  return (
    <Dialog open={props.open} onOpenChange={(next) => !next && props.onClose()}>
      <DialogContent className="max-h-[90dvh] overflow-y-auto">{props.open && <RuleForm {...props} />}</DialogContent>
    </Dialog>
  )
}

const THRESHOLD_LABELS: Partial<Record<RuleKind, string>> = {
  no_data: 'Segundos sin reportar',
  pending_updates: 'Pendientes de seguridad (mínimo)',
  reboot_pending: 'Días con reinicio pendiente',
  os_eol: 'Días de antelación al fin de vida',
}

function CheckField({ id, label, hint, checked, onChange }: { id: string; label: string; hint?: string; checked: boolean; onChange: (v: boolean) => void }) {
  return (
    <label htmlFor={id} className="flex items-start gap-2 text-sm">
      <Checkbox id={id} className="mt-0.5" checked={checked} onCheckedChange={(v) => onChange(v === true)} />
      <span>
        {label}
        {hint && <span className="block text-xs text-text-muted">{hint}</span>}
      </span>
    </label>
  )
}

function RuleForm({ rule, onClose, onSaved }: RuleFormDialogProps) {
  const editing = rule !== null
  const [draft, setDraft] = useState<RuleDraft>(() => ruleToDraft(rule))
  const [errors, setErrors] = useState<RuleErrors>({})
  const [channelIds, setChannelIds] = useState<string[]>(() => rule?.channel_ids ?? [])
  const [createdId, setCreatedId] = useState<string | null>(null)
  const { error, busy, run } = useUserMutation()
  const set = <K extends keyof RuleDraft>(key: K, value: RuleDraft[K]) => setDraft((d) => ({ ...d, [key]: value }))

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    const built = buildRuleInput(draft, editing)
    if ('errors' in built) return setErrors(built.errors)
    setErrors({})
    const { input } = built
    const ok = await run(async () => {
      // Si la regla ya se creó y falló solo la asignación de canales, el reintento no la duplica.
      let id = rule?.id ?? createdId
      if (rule) await updateRule(rule.id, input)
      else if (!id) {
        id = (
          await createRule({
            ...input,
            kind: draft.kind,
            ...(draft.kind === 'metric' && { metric: draft.metric }),
            ...(draft.kind === 'service_check' && { check_id: draft.checkId }),
          })
        ).id
        setCreatedId(id)
      }
      const before = rule?.channel_ids ?? []
      const changed = channelIds.length !== before.length || channelIds.some((c) => !before.includes(c))
      if (id && (changed || !rule)) await setRuleChannels(id, channelIds)
    }, (err) => (err.code === 'check_not_found' ? 'El chequeo elegido ya no existe. Elige otro.' : null))
    if (ok) onSaved()
  }

  const isMetric = draft.kind === 'metric'
  const isCheck = draft.kind === 'service_check'
  const updates = isUpdatesKind(draft.kind)
  const thresholdLabel = THRESHOLD_LABELS[draft.kind] ?? (isLoadMetric(draft.metric) ? 'Umbral' : 'Umbral (%)')

  return (
    <form onSubmit={(e) => void handleSubmit(e)} className="grid gap-4" noValidate>
      <DialogHeader>
        <DialogTitle>{editing ? 'Editar regla' : 'Nueva regla'}</DialogTitle>
        <DialogDescription>
          {editing ? 'El tipo y la métrica no se pueden cambiar; crea otra regla si los necesitas distintos.' : 'Se evalúa cada 15 segundos sobre los hosts del alcance.'}
        </DialogDescription>
      </DialogHeader>
      <FormField
        id="rule-name"
        label="Nombre"
        value={draft.name}
        onChange={(e) => set('name', e.target.value)}
        maxLength={MAX_NAME}
        autoComplete="off"
        autoFocus
        error={errors.name}
      />
      <SelectField
        id="rule-kind"
        label="Tipo"
        value={draft.kind}
        onChange={(v) => {
          set('kind', v as RuleKind)
          // Un alcance que el nuevo tipo no admite (p. ej. dispositivo en una regla de metricas) vuelve a Todos.
          if (!ruleScopeTypes(v).includes(draft.scope.type)) set('scope', ALL_SCOPE)
        }}
        options={KIND_OPTIONS}
        disabled={editing}
      />
      {isMetric && (
        <div className="grid gap-3 sm:grid-cols-2">
          <SelectField
            id="rule-metric"
            label="Métrica"
            value={draft.metric}
            onChange={(v) => set('metric', v as RuleMetric)}
            options={METRIC_OPTIONS}
            disabled={editing}
          />
          <SelectField id="rule-op" label="Condición" value={draft.op} onChange={(v) => set('op', v as RuleOp)} options={OP_OPTIONS} />
        </div>
      )}
      {isCheck && (
        <>
          <CheckSelectField
            id="rule-check"
            value={draft.checkId}
            onChange={(v) => set('checkId', v)}
            currentName={rule?.check_name}
            disabled={editing}
            error={errors.check}
          />
          <CheckField
            id="rule-warn-or-down"
            label="Alertar también en advertencia"
            hint="Sin marcar, solo alerta cuando el chequeo está caído. El alcance es el del chequeo."
            checked={draft.warnOrDown}
            onChange={(v) => set('warnOrDown', v)}
          />
        </>
      )}
      {draft.kind === 'exposed_port' && (
        <FormField
          id="rule-ports"
          label="Puertos esperados"
          value={draft.expectedPorts}
          onChange={(e) => set('expectedPorts', e.target.value)}
          placeholder="tcp/22, udp/53"
          autoComplete="off"
          hint="Los puertos de la primera foto del host no alertan; aquí lista los puertos nuevos que sí son normales. Separa con comas o espacios."
          error={errors.ports}
        />
      )}
      {(isMetric || draft.kind === 'no_data' || updates) && (
        <div className="grid gap-3 sm:grid-cols-2">
          <FormField
            id="rule-threshold"
            label={thresholdLabel}
            inputMode="decimal"
            value={draft.threshold}
            onChange={(e) => set('threshold', e.target.value)}
            error={errors.threshold}
          />
          {isMetric && (
            <FormField
              id="rule-clear"
              label="Umbral de recuperación"
              inputMode="decimal"
              value={draft.clear}
              onChange={(e) => set('clear', e.target.value)}
              hint="Opcional: evita que la alerta oscile."
              error={errors.clear}
            />
          )}
        </div>
      )}
      <div className="grid gap-3 sm:grid-cols-2">
        {draft.kind === 'pending_updates' ? (
          <FormField
            id="rule-duration-days"
            label="Duración (días)"
            inputMode="numeric"
            value={draft.durationDays}
            onChange={(e) => set('durationDays', e.target.value)}
            hint="Días que deben mantenerse los pendientes. 0 = de inmediato."
            error={errors.durationDays}
          />
        ) : (
          !updates && (
            <FormField
              id="rule-duration"
              label="Duración (minutos)"
              inputMode="decimal"
              value={draft.durationMin}
              onChange={(e) => set('durationMin', e.target.value)}
              hint="Tiempo que debe mantenerse la condición. 0 = de inmediato."
              error={errors.duration}
            />
          )
        )}
        <FormField
          id="rule-resolve"
          label="Espera de resolución (minutos)"
          inputMode="decimal"
          value={draft.resolveMin}
          onChange={(e) => set('resolveMin', e.target.value)}
          hint="Tiempo sano antes de resolverla."
          error={errors.resolve}
        />
      </div>
      {!isCheck && <ScopeFields id="rule" types={ruleScopeTypes(draft.kind)} value={draft.scope} onChange={(s) => set('scope', s)} error={errors.scope} />}
      <SelectField
        id="rule-severity"
        label="Severidad"
        value={draft.severity}
        onChange={(v) => set('severity', v as AlertSeverity)}
        options={SEVERITY_OPTIONS}
      />
      {!isCheck && (
        <CheckField
          id="rule-offline"
          label="Incluir equipos sin conexión"
          hint="Se evalúan con el último valor conocido."
          checked={draft.includeOffline}
          onChange={(v) => set('includeOffline', v)}
        />
      )}
      <CheckField
        id="rule-enabled"
        label="Regla activa"
        hint="Desactivada no genera alertas nuevas y resuelve las abiertas."
        checked={draft.enabled}
        onChange={(v) => set('enabled', v)}
      />
      <ChannelPicker selected={channelIds} onChange={setChannelIds} />
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
