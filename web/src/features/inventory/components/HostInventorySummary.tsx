import { useCallback, useEffect, useState } from 'react'
import { IconArrowRight, IconListDetails } from '@tabler/icons-react'
import { Link } from 'react-router'
import { Button } from '@/components/ui/button'
import { StatCard, StatValue } from '@/components/ui/stat-card'
import { useAuth } from '@/features/auth/hooks/useAuth'
import { useNow } from '@/hooks/useNow'
import { useInventorySummary } from '../hooks/useInventory'
import type { InventoryModule } from '../types/inventory.types'
import { canViewInventory, inventoryPath, MODULE_LABELS, summaryValue } from '../utils/inventory'

const MODULES: readonly InventoryModule[] = ['ports', 'services', 'processes']

function SummaryCards({ hostId, onDenied }: { hostId: string; onDenied: () => void }) {
  const now = useNow(30_000)
  const { data, errorStatus, error } = useInventorySummary(hostId)
  useEffect(() => {
    if (errorStatus === 404) onDenied()
  }, [errorStatus, onDenied])
  if (errorStatus === 404) return null
  // Un error distinto de 404 no muestra nada: el detalle del host no debe llenarse de avisos secundarios.
  if (error && !data) return null
  return (
    <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
      {MODULES.map((m) => {
        const mod = data?.modules[m]
        const { text, detail } = summaryValue(mod, now)
        return (
          <StatCard key={m} label={MODULE_LABELS[m]} Icon={IconListDetails}>
            <StatValue pending={!data} detail={detail}>
              {text}
            </StatValue>
          </StatCard>
        )
      })}
    </div>
  )
}

/** Resumen de inventario en el detalle del host: solo admin o `can_inventory`; un 404 lo oculta. Una sola consulta. */
export function HostInventorySummary({ hostId }: { hostId: string }) {
  const { user } = useAuth()
  const [denied, setDenied] = useState(false)
  const onDenied = useCallback(() => setDenied(true), [])
  if (!canViewInventory(user) || denied) return null
  return (
    <section className="flex flex-col gap-3" aria-labelledby="host-inventory-title">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 id="host-inventory-title" className="text-sm font-semibold">
          Inventario
        </h2>
        <Button asChild variant="outline" size="sm">
          <Link to={inventoryPath(hostId)}>
            Ver inventario
            <IconArrowRight stroke={1.5} aria-hidden />
          </Link>
        </Button>
      </div>
      <SummaryCards hostId={hostId} onDenied={onDenied} />
    </section>
  )
}
