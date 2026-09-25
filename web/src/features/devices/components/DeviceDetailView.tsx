import { useState } from 'react'
import { IconPencil, IconTrash } from '@tabler/icons-react'
import { Link, useNavigate } from 'react-router'
import { Button } from '@/components/ui/button'
import { InlineError } from '@/components/ui/inline-error'
import { HostChecksSection } from '@/features/checks/components/HostChecksSection'
import { useAuth } from '@/features/auth/hooks/useAuth'
import { BackToHostsLink } from '@/features/hosts/components/BackToHostsLink'
import { Item } from '@/features/hosts/components/HostSystemInfo'
import { formatFullDate, formatLastSeen } from '@/features/hosts/utils/format'
import { useDeviceDetail } from '../hooks/useDevices'
import { deviceStateOverride } from '../utils/status'
import { DeleteDeviceConfirm } from './DeleteDeviceConfirm'
import { DeviceFormDialog } from './DeviceFormDialog'
import { DeviceKindBadge, DeviceStatusBadge } from './DeviceBadges'

/** Detalle sobrio de un dispositivo sin agente: sin metricas ni inventario, solo su estado y sus chequeos ping. */
export function DeviceDetailView({ id }: { id: string }) {
  const { user } = useAuth()
  const isAdmin = user?.role === 'admin'
  const navigate = useNavigate()
  const detail = useDeviceDetail(id)
  const [editing, setEditing] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const device = detail.data
  const override = device ? deviceStateOverride(device) : null

  return (
    <div className="flex flex-col gap-6">
      <BackToHostsLink to="/dispositivos">Dispositivos</BackToHostsLink>
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
                    <IconPencil stroke={1.5} aria-hidden />
                    Editar
                  </Button>
                  <Button variant="outline" size="sm" onClick={() => setDeleting(true)}>
                    <IconTrash stroke={1.5} aria-hidden />
                    Eliminar
                  </Button>
                </div>
              )}
            </div>
            {override && <p className="text-sm">{override.reason}: no se está evaluando su estado.</p>}
          </div>
          <dl className="grid gap-x-6 gap-y-3 rounded-md border border-border bg-surface p-4 sm:grid-cols-2">
            {isAdmin && <Item term="Dirección">{device.address}</Item>}
            {isAdmin && (
              <Item term="Colector">
                {device.collector ? (
                  <Link to={`/hosts/${device.collector.id}`} className="rounded-md text-brand outline-none hover:underline focus-visible:ring-2 focus-visible:ring-ring">
                    {device.collector.name}
                  </Link>
                ) : null}
              </Item>
            )}
            <Item term="Grupo">{device.group?.name ?? 'Sin grupo'}</Item>
            <Item term="Visto">{device.last_seen_at ? formatLastSeen(device.last_seen_at) : 'Sin respuestas todavía'}</Item>
            <Item term="Estado desde">{formatFullDate(device.status_changed_at)}</Item>
            <Item term="Creado">{formatFullDate(device.created_at)}</Item>
          </dl>
          <HostChecksSection hostId={id} />
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
