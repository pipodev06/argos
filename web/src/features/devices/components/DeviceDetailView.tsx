import { useState } from 'react'
import { PencilIcon, PauseIcon, PlayIcon, Trash2Icon } from 'lucide-react'
import { useNavigate } from 'react-router'
import { ViewAction } from '@/components/data-table/RowActions'
import { Button } from '@/components/ui/button'
import { InlineError } from '@/components/ui/inline-error'
import { HostChecksSection } from '@/features/checks/components/HostChecksSection'
import { useAuth } from '@/features/auth/hooks/useAuth'
import { BackLink } from '@/components/ui/back-link'
import { Item } from '@/features/hosts/components/HostSystemInfo'
import { formatFullDate, formatLastSeen } from '@/features/hosts/utils/format'
import { versionLabel } from '@/features/snmp-profiles/utils/profileForm'
import { useDeviceEnabled } from '../hooks/useDeviceEnabled'
import { useDeviceDetail } from '../hooks/useDevices'
import { deviceStateOverride } from '../utils/status'
import { DeleteDeviceConfirm } from './DeleteDeviceConfirm'
import { DeviceFormDialog } from './DeviceFormDialog'
import { CollectorMismatchNotice, DeviceKindBadge, DeviceStatusBadge } from './DeviceBadges'

/** Detalle sobrio de un dispositivo sin agente: sin metricas ni inventario, solo su estado y sus chequeos ping. */
export function DeviceDetailView({ id }: { id: string }) {
  const { user } = useAuth()
  const isAdmin = user?.role === 'admin'
  const navigate = useNavigate()
  const detail = useDeviceDetail(id)
  const enabling = useDeviceEnabled(() => void detail.refresh())
  const [editing, setEditing] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const device = detail.data
  const override = device ? deviceStateOverride(device) : null

  return (
    <div className="flex flex-col gap-6">
      <BackLink to="/dispositivos">Dispositivos</BackLink>
      {detail.errorStatus === 404 && <InlineError>Dispositivo no encontrado.</InlineError>}
      {detail.error && detail.errorStatus !== 404 && <InlineError>{detail.error}</InlineError>}
      {!device && detail.loading && <p className="text-sm text-text-muted">Cargando…</p>}
      {device && (
        <>
          <div className="flex flex-col gap-2">
            <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
              <h1 className="min-w-0 truncate text-lg font-semibold">{device.name}</h1>
              <DeviceKindBadge />
              <DeviceStatusBadge device={device} />
              {isAdmin && (
                <div className="ml-auto flex gap-2">
                  <Button variant="outline" size="sm" onClick={() => setEditing(true)}>
                    <PencilIcon strokeWidth={1.5} aria-hidden />
                    Editar
                  </Button>
                  <Button variant="outline" size="sm" disabled={enabling.busy} onClick={() => void enabling.toggle(device)}>
                    {device.status === 'disabled' ? <PlayIcon strokeWidth={1.5} aria-hidden /> : <PauseIcon strokeWidth={1.5} aria-hidden />}
                    {device.status === 'disabled' ? 'Habilitar' : 'Deshabilitar'}
                  </Button>
                  <Button variant="outline" size="sm" onClick={() => setDeleting(true)}>
                    <Trash2Icon strokeWidth={1.5} aria-hidden />
                    Eliminar
                  </Button>
                </div>
              )}
            </div>
            {isAdmin && device.collector_group_mismatch && <p className="text-sm"><CollectorMismatchNotice /></p>}
            {override && <p className="text-sm">{override.reason}: no se está evaluando su estado.</p>}
            {enabling.error && <InlineError>{enabling.error}</InlineError>}
            {device.status === 'disabled' && <p className="text-sm text-text-muted">Deshabilitado: el colector no lo sondea y sus alertas se resolvieron. Habilítalo para volver a monitorearlo.</p>}
          </div>
          <dl className="grid gap-x-6 gap-y-3 rounded-md border border-border bg-surface p-4 sm:grid-cols-2">
            {isAdmin && <Item term="Dirección">{device.address}</Item>}
            {isAdmin && (
              <Item term="Colector">
                {device.collector ? (
                  <span className="inline-flex items-center gap-1">
                    {device.collector.name}
                    <ViewAction to={`/hosts/${device.collector.id}`} label={device.collector.name} />
                  </span>
                ) : null}
              </Item>
            )}
            {isAdmin && (
              <Item term="Perfil SNMP">
                {device.snmp_profile ? `${device.snmp_profile.name} (${versionLabel(device.snmp_profile.version)})` : 'Sin perfil'}
              </Item>
            )}
            <Item term="Grupo">{device.group?.name ?? 'Sin grupo'}</Item>
            <Item term="Visto">{device.last_seen_at ? formatLastSeen(device.last_seen_at) : 'Sin respuestas'}</Item>
            <Item term="Estado desde">{formatFullDate(device.status_changed_at)}</Item>
            <Item term="Creado">{formatFullDate(device.created_at)}</Item>
          </dl>
          <HostChecksSection hostId={id} targetFallback={isAdmin ? device.address : undefined} />
          {isAdmin && (
            <>
              <DeviceFormDialog
                device={device}
                open={editing}
                onClose={() => setEditing(false)}
                onSaved={() => {
                  setEditing(false)
                  void detail.refresh()
                }}
              />
              <DeleteDeviceConfirm
                device={deleting ? device : null}
                onClose={() => setDeleting(false)}
                onDeleted={() => void navigate('/dispositivos')}
              />
            </>
          )}
        </>
      )}
    </div>
  )
}
