import { useState, type FormEvent } from 'react'
import { TriangleAlertIcon } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { FormField } from '@/components/ui/form-field'
import { FormHelp, type HelpItem } from '@/components/ui/form-help'
import { InlineError } from '@/components/ui/inline-error'
import { SelectField } from '@/components/ui/select-field'
import { useUserMutation } from '@/features/users/hooks/useUserMutation'
import { createSnmpProfile, updateSnmpProfile } from '../services/snmpProfiles.service'
import type { SnmpAuthProto, SnmpPrivProto, SnmpProfile } from '../types/snmpProfile.types'
import { mapSnmpProfileError } from '../utils/errors'
import {
  AUTH_OPTIONS,
  buildProfileInput,
  MAX_COMMUNITY,
  MAX_NAME,
  MAX_PASS,
  MAX_USER,
  PRIV_OPTIONS,
  profileToDraft,
  VERSION_OPTIONS,
  type ProfileDraft,
  type ProfileErrors,
} from '../utils/profileForm'

interface SnmpProfileFormDialogProps {
  /** null = crear; un perfil = editar. */
  profile: SnmpProfile | null
  open: boolean
  onClose: () => void
  onSaved: () => void
}

const SNMP_HELP: readonly HelpItem[] = [
  ['Perfil SNMP', 'Credenciales de solo lectura que el colector usa para consultar los dispositivos.'],
  ['Versión', 'No se puede cambiar después: crea otro perfil si necesitas otra.'],
  ['Contraseñas y comunidad', `De 8 a ${MAX_PASS} caracteres. Se guardan cifradas y no se vuelven a mostrar; al editar, vacío conserva lo guardado.`],
]

export function SnmpProfileFormDialog(props: SnmpProfileFormDialogProps) {
  // El formulario solo existe con el diálogo abierto: al cerrar, las contraseñas y la comunidad se descartan.
  return (
    <Dialog open={props.open} onOpenChange={(next) => !next && props.onClose()}>
      <DialogContent className="max-h-[90dvh] overflow-y-auto">{props.open && <ProfileForm {...props} />}</DialogContent>
    </Dialog>
  )
}

function ProfileForm({ profile, onClose, onSaved }: SnmpProfileFormDialogProps) {
  const editing = profile !== null
  const [draft, setDraft] = useState<ProfileDraft>(() => profileToDraft(profile))
  const [errors, setErrors] = useState<ProfileErrors>({})
  const { error, busy, run } = useUserMutation()
  const set = <K extends keyof ProfileDraft>(key: K, value: ProfileDraft[K]) => setDraft((d) => ({ ...d, [key]: value }))
  const text = (key: 'name' | 'username' | 'authPass' | 'privPass' | 'community') => ({
    value: draft[key],
    onChange: (e: { target: { value: string } }) => set(key, e.target.value),
    error: errors[key],
  })
  const v3 = draft.version === '3'

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    const built = buildProfileInput(draft, profile)
    if ('errors' in built) return setErrors(built.errors)
    setErrors({})
    const { input } = built
    if (await run(() => (profile ? updateSnmpProfile(profile.id, input) : createSnmpProfile(input)), mapSnmpProfileError)) onSaved()
  }

  return (
    <form onSubmit={(e) => void handleSubmit(e)} className="grid gap-3" noValidate autoComplete="off">
      <DialogHeader className="flex-row items-center gap-1">
        <DialogTitle>{editing ? 'Editar perfil SNMP' : 'Nuevo perfil SNMP'}</DialogTitle>
        <FormHelp title="Perfil SNMP" items={SNMP_HELP} />
      </DialogHeader>
      <SelectField
        id="snmp-version"
        label="Versión"
        value={draft.version}
        onChange={(v) => set('version', v as ProfileDraft['version'])}
        options={VERSION_OPTIONS}
        disabled={editing}
      />
      <FormField id="snmp-name" label="Nombre" maxLength={MAX_NAME} autoComplete="off" autoFocus {...text('name')} />

      {v3 ? (
        <>
          <FormField id="snmp-user" label="Usuario" maxLength={MAX_USER} autoComplete="off" spellCheck={false} {...text('username')} />
          <div className="grid gap-3 sm:grid-cols-2">
            <SelectField id="snmp-auth-proto" label="Protocolo de autenticación" value={draft.authProto} onChange={(v) => set('authProto', v as SnmpAuthProto)} options={AUTH_OPTIONS} />
            <SelectField id="snmp-priv-proto" label="Protocolo de cifrado" value={draft.privProto} onChange={(v) => set('privProto', v as SnmpPrivProto)} options={PRIV_OPTIONS} />
          </div>
          <FormField
            id="snmp-auth-pass"
            label="Contraseña de autenticación"
            type="password"
            maxLength={MAX_PASS}
            autoComplete="new-password"
            placeholder={editing ? 'Sin cambios' : undefined}
            {...text('authPass')}
          />
          <FormField
            id="snmp-priv-pass"
            label="Contraseña de cifrado"
            type="password"
            maxLength={MAX_PASS}
            autoComplete="new-password"
            placeholder={editing ? 'Sin cambios' : undefined}
            {...text('privPass')}
          />
        </>
      ) : (
        <>
          <div role="alert" className="flex items-start gap-2 rounded-md border border-border bg-warn/10 p-3 text-sm">
            <TriangleAlertIcon className="mt-0.5 size-4 shrink-0 text-warn" strokeWidth={1.5} aria-hidden />
            <p>
              <strong className="font-semibold">SNMPv2c es menos seguro.</strong> La comunidad viaja sin cifrar por la red y cualquiera que la vea puede reutilizarla.
              Solo lectura. Usa SNMPv3 siempre que el equipo lo permita.
            </p>
          </div>
          {!editing && (
            <div className="grid gap-1">
              <label htmlFor="snmp-allow-v2c" className="flex items-start gap-2 text-sm">
                <Checkbox
                  id="snmp-allow-v2c"
                  className="mt-0.5"
                  checked={draft.allowV2c}
                  aria-invalid={errors.allowV2c ? true : undefined}
                  onCheckedChange={(v) => set('allowV2c', v === true)}
                />
                <span>Entiendo que la comunidad viaja sin cifrar y quiero usar SNMPv2c</span>
              </label>
              {errors.allowV2c && <InlineError>{errors.allowV2c}</InlineError>}
            </div>
          )}
          <FormField
            id="snmp-community"
            label="Comunidad"
            type="password"
            maxLength={MAX_COMMUNITY}
            autoComplete="new-password"
            placeholder={editing ? 'Sin cambios' : undefined}
            {...text('community')}
          />
        </>
      )}
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
