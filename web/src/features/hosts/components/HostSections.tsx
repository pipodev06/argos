import { useCallback, useState } from 'react'
import { Navigate } from 'react-router'
import { InlineError } from '@/components/ui/inline-error'
import { Skeleton } from '@/components/ui/skeleton'
import { HostEventsView } from '@/features/alerts/components/HostEventsView'
import { useAuth } from '@/features/auth/hooks/useAuth'
import { HostChecksSection } from '@/features/checks/components/HostChecksSection'
import { InventoryContent } from '@/features/inventory/components/InventoryContent'
import { canViewInventory } from '@/features/inventory/utils/inventory'
import { HostUpdatesSection } from '@/features/updates/components/HostUpdatesSection'
import { canViewUpdates } from '@/features/updates/utils/updates'
import { useHostOutlet } from '../hooks/useHostOutlet'
import { hostTabPath } from '../utils/tabs'
import { CollectorDevices } from './CollectorDevices'
import { HostDisks } from './HostDisks'
import { HostLatestCards } from './HostLatestCards'
import { HostOpenAlerts } from './HostOpenAlerts'
import { HostSystemInfo } from './HostSystemInfo'

// Sub-rutas livianas del host (sin graficas); Métricas vive aparte (HostMetricsTab) para cargar Recharts lazy.

export function HostSummaryTab() {
  const { hostId, detail } = useHostOutlet()
  const data = detail.data
  return (
    <div className="flex flex-col gap-3">
      <HostLatestCards
        latest={data?.latest ?? null}
        info={data?.info ?? null}
        status={data?.host.status ?? null}
        pending={detail.loading}
      />
      <HostOpenAlerts hostId={hostId} />
      {data && <HostSystemInfo host={data.host} info={data.info} />}
      {data && <HostDisks disks={data.disks} physical={data.info.physical_disks} />}
    </div>
  )
}

export function HostChecksTab() {
  const { hostId } = useHostOutlet()
  return <HostChecksSection hostId={hostId} title={false} />
}

export function HostDevicesTab() {
  const { hostId, isCollector } = useHostOutlet()
  if (isCollector === null) return <Skeleton className="h-12 w-full" />
  if (!isCollector) return <Navigate to={hostTabPath(hostId, '')} replace />
  return <CollectorDevices hostId={hostId} />
}

export function HostUpdatesTab() {
  const { user } = useAuth()
  const { hostId } = useHostOutlet()
  if (!canViewUpdates(user)) return <Navigate to={hostTabPath(hostId, '')} replace />
  return <HostUpdatesSection hostId={hostId} />
}

export function HostInventoryTab() {
  const { user } = useAuth()
  const { hostId } = useHostOutlet()
  const [denied, setDenied] = useState(false)
  const onDenied = useCallback(() => setDenied(true), [])
  if (denied || !canViewInventory(user)) return <InlineError>Inventario no disponible.</InlineError>
  return <InventoryContent hostId={hostId} onDenied={onDenied} />
}

export function HostEventsTab() {
  const { hostId } = useHostOutlet()
  return <HostEventsView id={hostId} />
}
