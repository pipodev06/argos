import { useState, type FormEvent } from 'react'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { FormField } from '@/components/ui/form-field'
import { FormHelp, type HelpItem } from '@/components/ui/form-help'
import { InlineError } from '@/components/ui/inline-error'
import { SelectField } from '@/components/ui/select-field'
import { useGroups } from '@/features/groups/hooks/useGroups'
import { useSnmpProfileOptions } from '@/features/snmp-profiles/hooks/useSnmpProfiles'
import { versionLabel } from '@/features/snmp-profiles/utils/profileForm'
import { useUserMutation } from '@/features/users/hooks/useUserMutation'
import { useCollectors } from '../hooks/useDevices'
import { createDevice, updateDevice } from '../services/devices.service'
import type { Device } from '../types/device.types'
import { buildDeviceInput, deviceToDraft, mapDeviceError, MAX_NAME, type DeviceDraft, type DeviceErrors } from '../utils/deviceForm'

interface DeviceFormDialogProps {
  /** null = crear; un dispositivo = editar. */
  device: Device | null
  open: boolean
  onClose: () => void
  onSaved: () => void
}

const NO_GROUP = '__none__'
const NO_PROFILE = '__none__'

const DEVICE_HELP: readonly HelpItem[] = [
  ['Dispositivo', 'Equipo sin agente (impresora, switch, UPS…): un host con agente, el colector, le hace ping y su estado depende de las respuestas.'],
  ['IP', 'IPv4 o IPv6, no un nombre. Debe estar en las redes permitidas del colector.'],
  ['Colector', 'Host con agente que hace el ping.'],
  ['Perfil SNMP', 'Opcional. Sin perfil no recibe chequeos SNMP. Los perfiles se crean en Configuración.'],
]

export function DeviceFormDialog(props: DeviceFormDialogProps) {
  // El formulario solo existe con el dialogo abierto: cada apertura empieza limpia y recarga colectores y grupos.
  return (
    <Dialog open={props.open} onOpenChange={(next) => !next && props.onClose()}>
      <DialogContent className="max-h-[90dvh] overflow-y-auto">{props.open && <DeviceForm {...props} />}</DialogContent>
    </Dialog>
  )
}

function DeviceForm({ device, onClose, onSaved }: DeviceFormDialogProps) {
  const editing = device !== null
  const [draft, setDraft] = useState<DeviceDraft>(() => deviceToDraft(device))
  const [errors, setErrors] = useState<DeviceErrors>({})
  const { error, busy, run } = useUserMutation()
  const collectors = useCollectors()
  const { groups } = useGroups()
  const profiles = useSnmpProfileOptions()
  const set = <K extends keyof DeviceDraft>(key: K, value: DeviceDraft[K]) => setDraft((d) => ({ ...d, [key]: value }))

  // El colector actual se conserva como opcion aunque ya no sea elegible (p. ej. deshabilitado): el servidor decide.
  const collectorOptions = (collectors.data ?? []).map((c) => ({ value: c.id, label: c.name }))
  if (device?.collector && !collectorOptions.some((o) => o.value === device.collector?.id)) {
    collectorOptions.unshift({ value: device.collector.id, label: device.collector.name })
  }
  const noCollectors = collectors.data !== null && collectorOptions.length === 0
  const groupOptions = [{ value: NO_GROUP, label: 'Sin grupo' }, ...(groups?.items ?? []).map((g) => ({ value: g.id, label: g.name }))]
  // El perfil actual se conserva como opción aunque la lista no lo traiga; el servidor decide.
  const profileOptions = [
    { value: NO_PROFILE, label: 'Sin perfil' },
    ...(profiles.data ?? []).map((p) => ({ value: p.id, label: `${p.name} (${versionLabel(p.version)})` })),
  ]
  if (device?.snmp_profile && !profileOptions.some((o) => o.value === device.snmp_profile?.id)) {
    profileOptions.push({ value: device.snmp_profile.id, label: device.snmp_profile.name })
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    const built = buildDeviceInput(draft, device)
    if ('errors' in built) return setErrors(built.errors)
    setErrors({})
    const { input } = built
    if (await run(() => (device ? updateDevice(device.id, input) : createDevice(input)), mapDeviceError)) onSaved()
  }

  return (
    <form onSubmit={(e) => void handleSubmit(e)} className="grid gap-3" noValidate autoComplete="off">
      <DialogHeader className="flex-row items-center gap-1">
        <DialogTitle>{editing ? 'Editar dispositivo' : 'Nuevo dispositivo'}</DialogTitle>
        <FormHelp title="Dispositivo" items={DEVICE_HELP} />
      </DialogHeader>
      <FormField
        id="device-name"
        label="Nombre"
        value={draft.name}
        onChange={(e) => set('name', e.target.value)}
        maxLength={MAX_NAME}
        autoComplete="off"
        autoFocus
        error={errors.name}
      />
      <FormField
        id="device-address"
        label="Dirección IP"
        value={draft.address}
        onChange={(e) => set('address', e.target.value)}
        placeholder="192.168.1.10"
        autoComplete="off"
        error={errors.address}
      />
      <SelectField
        id="device-collector"
        label="Colector"
        value={draft.collectorId}
        onChange={(v) => set('collectorId', v)}
        options={collectorOptions}
        placeholder={collectors.data ? 'Elige un colector' : 'Cargando…'}
        error={errors.collectorId}
      />
      {noCollectors && (
        <InlineError>
          No hay hosts con agente que puedan hacer de colector. Enrola un host y define ARGOS_PROBE_ALLOW en su agente (las IP o redes que puede sondear).
        </InlineError>
      )}
      {collectors.error && <InlineError>{collectors.error}</InlineError>}
      <SelectField
        id="device-group"
        label="Grupo"
        value={draft.groupId || NO_GROUP}
        onChange={(v) => set('groupId', v === NO_GROUP ? '' : v)}
        options={groupOptions}
      />
      <SelectField
        id="device-snmp-profile"
        label="Perfil SNMP"
        value={draft.snmpProfileId || NO_PROFILE}
        onChange={(v) => set('snmpProfileId', v === NO_PROFILE ? '' : v)}
        options={profileOptions}
      />
      {profiles.error && <InlineError>{profiles.error}</InlineError>}
      {error && <InlineError>{error}</InlineError>}
      <DialogFooter>
        <Button type="button" variant="ghost" onClick={onClose}>
          Cancelar
        </Button>
        <Button type="submit" disabled={busy || noCollectors}>
          {busy ? 'Guardando...' : 'Guardar'}
        </Button>
      </DialogFooter>
    </form>
  )
}
