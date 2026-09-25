import { useState, type FormEvent } from 'react'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { FormField } from '@/components/ui/form-field'
import { InlineError } from '@/components/ui/inline-error'
import { SelectField } from '@/components/ui/select-field'
import { ScopeFields } from '@/features/alerts/components/ScopeFields'
import { ALL_SCOPE } from '@/features/alerts/utils/scope'
import { mapChannelError } from '@/features/channels/utils/errors'
import { useUserMutation } from '@/features/users/hooks/useUserMutation'
import { createCheck, updateCheck } from '../services/checks.service'
import type { CheckType, ServiceCheck } from '../types/check.types'
import {
  buildCheckInput,
  CHECK_TYPE_OPTIONS,
  checkToDraft,
  HEADER_OPTIONS,
  MAX_EXPECT_TEXT,
  MAX_IF_INDEX,
  MAX_NAME,
  MAX_SECRET,
  MAX_URL,
  urlQueryLooksSecret,
  type CheckDraft,
  type CheckErrors,
} from '../utils/checkForm'
import { isDeviceCheck, SNMP_METRIC_OPTIONS } from '../utils/format'

interface CheckFormDialogProps {
  /** null = crear; un chequeo = editar. */
  check: ServiceCheck | null
  open: boolean
  onClose: () => void
  onSaved: () => void
}

const HEADER_SELECT = HEADER_OPTIONS.map((h) => ({ value: h, label: h }))
const NO_HEADER = '__none__'

export function CheckFormDialog(props: CheckFormDialogProps) {
  // El formulario solo existe con el diálogo abierto: al cerrar, el secreto y el resto de campos se descartan.
  return (
    <Dialog open={props.open} onOpenChange={(next) => !next && props.onClose()}>
      <DialogContent className="max-h-[90dvh] overflow-y-auto">{props.open && <CheckForm {...props} />}</DialogContent>
    </Dialog>
  )
}

function CheckForm({ check, onClose, onSaved }: CheckFormDialogProps) {
  const editing = check !== null
  const [draft, setDraft] = useState<CheckDraft>(() => checkToDraft(check))
  const [errors, setErrors] = useState<CheckErrors>({})
  const { error, busy, run } = useUserMutation()
  const set = <K extends keyof CheckDraft>(key: K, value: CheckDraft[K]) => setDraft((d) => ({ ...d, [key]: value }))
  const text = (key: keyof CheckDraft) => ({
    value: draft[key] as string,
    onChange: (e: { target: { value: string } }) => set(key, e.target.value as never),
    error: errors[key],
  })

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    const built = buildCheckInput(draft, check)
    if ('errors' in built) return setErrors(built.errors)
    setErrors({})
    const { input } = built
    const ok = await run(() => (check ? updateCheck(check.id, input) : createCheck(input)), mapChannelError)
    if (ok) onSaved()
  }

  // El alcance de un dispositivo solo vale para ping: al cambiar a otro tipo vuelve a "todos".
  function changeType(type: string) {
    setDraft((d) => ({ ...d, type: type as CheckType, scope: d.scope.type === 'device' && !isDeviceCheck(type as CheckType) ? ALL_SCOPE : d.scope }))
  }

  const hasSecret = check?.has_secret ?? false

  return (
    <form onSubmit={(e) => void handleSubmit(e)} className="grid gap-4" noValidate autoComplete="off">
      <DialogHeader>
        <DialogTitle>{editing ? 'Editar chequeo' : 'Nuevo chequeo'}</DialogTitle>
        <DialogDescription>
          Por seguridad el agente solo comprueba el propio equipo salvo que el host tenga una lista de permitidos local (ARGOS_CHECK_ALLOW; para ping, ARGOS_PROBE_ALLOW).
        </DialogDescription>
      </DialogHeader>
      <FormField id="check-name" label="Nombre" maxLength={MAX_NAME} autoComplete="off" autoFocus {...text('name')} />
      <SelectField
        id="check-type"
        label="Tipo"
        value={draft.type}
        onChange={changeType}
        options={CHECK_TYPE_OPTIONS}
        disabled={editing}
        hint={editing ? 'El tipo no se puede cambiar; crea otro chequeo si lo necesitas distinto.' : undefined}
      />

      {(draft.type === 'tcp' || draft.type === 'tls_cert') && (
        <div className="grid gap-3 sm:grid-cols-2">
          <FormField id="check-host" label="Host" autoComplete="off" {...text('host')} />
          <FormField id="check-port" label="Puerto" inputMode="numeric" autoComplete="off" {...text('port')} />
        </div>
      )}
      {draft.type === 'http' && (
        <>
          <FormField
            id="check-url"
            label="URL"
            type="url"
            maxLength={MAX_URL}
            autoComplete="off"
            hint="Sin usuario ni clave y sin tokens en la query: el token va en el campo secreto, nunca en la URL."
            {...text('url')}
          />
          {!errors.url && urlQueryLooksSecret(draft.url) && (
            <InlineError>La query parece llevar un token o credencial. Quítala de la URL y usa el secreto de cabecera.</InlineError>
          )}
          <div className="grid gap-3 sm:grid-cols-2">
            <FormField
              id="check-status"
              label="Estado esperado"
              placeholder="200-399"
              autoComplete="off"
              hint="Un código o un rango. Vacío = 200-399."
              {...text('expectStatus')}
            />
            <FormField
              id="check-text"
              label="Texto esperado"
              maxLength={MAX_EXPECT_TEXT}
              autoComplete="off"
              hint="Opcional: debe aparecer en la respuesta."
              {...text('expectText')}
            />
          </div>
        </>
      )}
      {draft.type === 'snmp' && (
        <>
          <SelectField
            id="check-metric"
            label="Métrica"
            value={draft.metric}
            onChange={(v) => set('metric', v)}
            options={SNMP_METRIC_OPTIONS}
            error={errors.metric}
          />
          {draft.metric === 'if_oper' && (
            <FormField
              id="check-if-index"
              label="Índice de interfaz (ifIndex)"
              inputMode="numeric"
              autoComplete="off"
              hint={`Entero de 1 a ${MAX_IF_INDEX}.`}
              {...text('ifIndex')}
            />
          )}
          <p className="text-xs text-text-muted">
            Un dispositivo sin perfil SNMP no recibe este chequeo: asígnale un perfil en su formulario. No lleva umbrales.
          </p>
        </>
      )}
      {draft.type === 'service' && <FormField id="check-service" label="Nombre del servicio" autoComplete="off" {...text('serviceName')} />}

      {(draft.type === 'tcp' || draft.type === 'http' || draft.type === 'ping') && (
        <div className="grid gap-3 sm:grid-cols-2">
          <FormField id="check-warn-ms" label="Advertencia (ms)" inputMode="numeric" hint="Opcional." autoComplete="off" {...text('warnMs')} />
          <FormField id="check-crit-ms" label="Crítico (ms)" inputMode="numeric" hint="Opcional." autoComplete="off" {...text('critMs')} />
        </div>
      )}
      {draft.type === 'tls_cert' && (
        <div className="grid gap-3 sm:grid-cols-2">
          <FormField id="check-warn-days" label="Advertencia (días restantes)" inputMode="numeric" autoComplete="off" {...text('warnDays')} />
          <FormField id="check-crit-days" label="Crítico (días restantes)" inputMode="numeric" autoComplete="off" {...text('critDays')} />
        </div>
      )}

      {draft.type === 'http' && (
        <fieldset className="grid gap-3 rounded-md border border-border p-3">
          <legend className="px-1 text-sm font-medium">Secreto de cabecera (opcional)</legend>
          <SelectField
            id="check-header"
            label="Cabecera"
            value={draft.headerName || NO_HEADER}
            onChange={(v) => set('headerName', v === NO_HEADER ? '' : v)}
            options={[{ value: NO_HEADER, label: 'Ninguna' }, ...HEADER_SELECT]}
            disabled={draft.removeSecret}
            error={errors.headerName}
          />
          {hasSecret && (
            <label htmlFor="check-remove-secret" className="flex items-start gap-2 text-sm">
              <Checkbox
                id="check-remove-secret"
                className="mt-0.5"
                checked={draft.removeSecret}
                onCheckedChange={(v) => setDraft((d) => ({ ...d, removeSecret: v === true, secret: '' }))}
              />
              <span>Quitar secreto</span>
            </label>
          )}
          <FormField
            id="check-secret"
            label="Secreto"
            type="password"
            maxLength={MAX_SECRET}
            autoComplete="new-password"
            disabled={draft.removeSecret}
            placeholder={hasSecret ? 'Hay un secreto guardado' : undefined}
            hint={hasSecret ? 'Déjalo vacío para conservar el guardado. Nunca se muestra.' : 'Se guarda cifrado y no se puede volver a ver.'}
            {...text('secret')}
          />
        </fieldset>
      )}

      <div className="grid gap-3 sm:grid-cols-3">
        <FormField id="check-interval" label="Intervalo (s)" inputMode="numeric" autoComplete="off" hint="30 a 86 400." {...text('interval')} />
        <FormField id="check-timeout" label="Timeout (ms)" inputMode="numeric" autoComplete="off" hint="200 a 30 000." {...text('timeout')} />
        <FormField id="check-retries" label="Reintentos" inputMode="numeric" autoComplete="off" hint="0 a 3." {...text('retries')} />
      </div>
      <ScopeFields id="check" ping={isDeviceCheck(draft.type)} hostScope={!isDeviceCheck(draft.type)} value={draft.scope} onChange={(s) => set('scope', s)} error={errors.scope} />
      <label htmlFor="check-enabled" className="flex items-start gap-2 text-sm">
        <Checkbox id="check-enabled" className="mt-0.5" checked={draft.enabled} onCheckedChange={(v) => set('enabled', v === true)} />
        <span>
          Chequeo activo
          <span className="block text-xs text-text-muted">Desactivado, el agente deja de ejecutarlo.</span>
        </span>
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
