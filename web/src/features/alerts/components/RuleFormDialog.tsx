import { useState, type FormEvent } from 'react'
import { Button } from '@/components/ui/button'
import { CheckField } from '@/components/ui/check-field'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { FormField } from '@/components/ui/form-field'
import { InlineError } from '@/components/ui/inline-error'
import { SelectField } from '@/components/ui/select-field'
import { CheckSelectField } from '@/features/checks/components/CheckSelectField'
import { mapScopeError } from '@/features/channels/utils/errors'
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
  RULE_TEMPLATES,
  cloneToDraft,
  ruleToDraft,
  type RuleDraft,
  type RuleErrors,
} from '../utils/ruleForm'
import { ScopeFields } from './ScopeFields'
import { ALL_SCOPE, ruleScopeTypes } from '../utils/scope'

interface RuleFormDialogProps {
  /** null = crear; una regla = editar (o, con `clone`, crear una copia precargada). */
  rule: AlertRule | null
  clone?: boolean
  open: boolean
  onClose: () => void
  onSaved: () => void
}

const METRIC_OPTIONS = (Object.keys(METRIC_LABELS) as RuleMetric[]).map((m) => ({ value: m, label: METRIC_LABELS[m] }))
const SEVERITY_OPTIONS = (Object.keys(SEVERITY_LABELS) as AlertSeverity[]).map((s) => ({ value: s, label: SEVERITY_LABELS[s] }))

const BLANK_TEMPLATE = 'blank'
const TEMPLATE_OPTIONS = [{ value: BLANK_TEMPLATE, label: 'En blanco' }, ...RULE_TEMPLATES.map((t) => ({ value: t.id, label: t.label }))]

export function RuleFormDialog(props: RuleFormDialogProps) {
  // El formulario se monta solo con el diálogo abierto: cada apertura empieza con el estado de la regla.
  return (
    <Dialog open={props.open} onOpenChange={(next) => !next && props.onClose()}>
      <DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-3xl">{props.open && <RuleForm {...props} />}</DialogContent>
    </Dialog>
  )
}

const THRESHOLD_LABELS: Partial<Record<RuleKind, string>> = {
  no_data: 'Segundos sin reportar',
  pending_updates: 'Pendientes de seguridad (mínimo)',
  reboot_pending: 'Días con reinicio pendiente',
  os_eol: 'Días de antelación al fin de vida',
}

function RuleForm({ rule: source, clone = false, onClose, onSaved }: RuleFormDialogProps) {
  // Al clonar, el formulario parte de la regla original pero se comporta como creación.
  const rule = clone ? null : source
  const editing = rule !== null
  const [draft, setDraft] = useState<RuleDraft>(() => (clone && source ? cloneToDraft(source) : ruleToDraft(rule)))
  const [templateId, setTemplateId] = useState(BLANK_TEMPLATE)
  const [errors, setErrors] = useState<RuleErrors>({})
  const [channelIds, setChannelIds] = useState<string[]>(() => source?.channel_ids ?? [])
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
      if (id && (changed || !rule)) {
        try {
          await setRuleChannels(id, channelIds)
        } catch (err) {
          if (!rule) {
            throw new Error(`La regla se creó, pero no se pudieron asignar los canales: ${err instanceof Error ? err.message : 'error desconocido'}. Reintenta para asignarlos.`)
          }
          throw err
        }
      }
    }, (err) => {
      if (err.code === 'check_not_found') return 'El chequeo elegido ya no existe. Elige otro.'
      // Nombre repetido (único global, sin distinguir mayúsculas): se marca en el campo.
      if (err.status === 409 || /name/i.test(err.code)) {
        setErrors({ name: 'Ya existe una regla con ese nombre. Elige otro.' })
        return 'Ya existe una regla con ese nombre.'
      }
      return mapScopeError(err)
    })
    if (ok) onSaved()
  }

  const isMetric = draft.kind === 'metric'
  const isCheck = draft.kind === 'service_check'
  const updates = isUpdatesKind(draft.kind)
  const thresholdLabel = THRESHOLD_LABELS[draft.kind] ?? (isLoadMetric(draft.metric) ? 'Umbral' : 'Umbral (%)')

  return (
    <form onSubmit={(e) => void handleSubmit(e)} className="grid gap-4 sm:grid-cols-2" noValidate>
      <DialogHeader className="sm:col-span-2">
        <DialogTitle>{editing ? 'Editar regla' : clone ? 'Duplicar regla' : 'Nueva regla'}</DialogTitle>
        <DialogDescription>
          {clone ? 'Se crea una regla nueva con los mismos valores y canales. Cambia el nombre si quieres.' : editing ?'El tipo y la métrica no se pueden cambiar; crea otra regla si los necesitas distintos.' : 'Se evalúa cada 15 segundos sobre los hosts del alcance.'}
        </DialogDescription>
      </DialogHeader>
      {!editing && !clone && (
        <div className="sm:col-span-2">
          <SelectField
            id="rule-template"
            label="Plantilla"
            value={templateId}
            onChange={(v) => {
              setTemplateId(v)
              const template = RULE_TEMPLATES.find((t) => t.id === v)
              setDraft(template ? template.apply() : ruleToDraft(null))
            }}
            options={TEMPLATE_OPTIONS}
          />
        </div>
      )}
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
        <>
          <SelectField
            id="rule-metric"
            label="Métrica"
            value={draft.metric}
            onChange={(v) => set('metric', v as RuleMetric)}
            options={METRIC_OPTIONS}
            disabled={editing}
          />
          <SelectField id="rule-op" label="Condición" value={draft.op} onChange={(v) => set('op', v as RuleOp)} options={OP_OPTIONS} />
        </>
      )}
      {isCheck && (
        <>
          <CheckSelectField
            id="rule-check"
            value={draft.checkId}
            onChange={(v) => set('checkId', v)}
            currentName={source?.check_name}
            disabled={editing}
            error={errors.check}
          />
          <div className="sm:col-span-2">
            <CheckField
              id="rule-warn-or-down"
              label="Alertar también en advertencia"
              hint="Sin marcar, solo alerta cuando el chequeo está caído. El alcance es el del chequeo."
              checked={draft.warnOrDown}
              onChange={(v) => set('warnOrDown', v)}
            />
          </div>
        </>
      )}
      {draft.kind === 'exposed_port' && (
        <div className="sm:col-span-2">
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
        </div>
      )}
      {(isMetric || draft.kind === 'no_data' || updates) && (
        <>
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
        </>
      )}
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
      {!isCheck && (
        <div className="sm:col-span-2">
          <ScopeFields id="rule" types={ruleScopeTypes(draft.kind)} value={draft.scope} onChange={(s) => set('scope', s)} error={errors.scope} />
        </div>
      )}
      <SelectField
        id="rule-severity"
        label="Severidad"
        value={draft.severity}
        onChange={(v) => set('severity', v as AlertSeverity)}
        options={SEVERITY_OPTIONS}
      />
      <div className="grid content-start gap-3">
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
      </div>
      <div className="sm:col-span-2">
        <ChannelPicker selected={channelIds} onChange={setChannelIds} />
      </div>
      {error && (
        <div className="sm:col-span-2">
          <InlineError>{error}</InlineError>
        </div>
      )}
      <DialogFooter className="sm:col-span-2">
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
