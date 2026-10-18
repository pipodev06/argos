import type { ReactNode } from 'react'
import { useSearchParams } from 'react-router'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { HostEventsSection } from '@/features/alerts/components/HostEventsSection'
import { useAuth } from '@/features/auth/hooks/useAuth'
import { HostChecksSection } from '@/features/checks/components/HostChecksSection'
import { InventoryContent } from '@/features/inventory/components/InventoryContent'
import { canViewInventory } from '@/features/inventory/utils/inventory'
import { HostUpdatesSection } from '@/features/updates/components/HostUpdatesSection'
import { canViewUpdates } from '@/features/updates/utils/updates'
import { CollectorDevices } from './CollectorDevices'

const TABS = ['resumen', 'metricas', 'chequeos', 'parches', 'inventario', 'historial'] as const
type Tab = (typeof TABS)[number]

/**
 * Secciones del detalle del host en pestañas (antes bloques sueltos uno debajo de otro). La pestaña va en la
 * URL (`?tab=`) y solo se monta la activa: cada una consulta su API al abrirse.
 */
export function HostTabs({ hostId, summary, metrics }: { hostId: string; summary: ReactNode; metrics: ReactNode }) {
  const { user } = useAuth()
  const [params, setParams] = useSearchParams()
  const visible = TABS.filter((t) => (t === 'parches' ? canViewUpdates(user) : t === 'inventario' ? canViewInventory(user) : true))
  const raw = params.get('tab') as Tab | null
  const tab: Tab = raw && visible.includes(raw) ? raw : 'resumen'

  function change(next: string) {
    setParams(
      (p) => {
        const out = new URLSearchParams(p)
        if (next === 'resumen') out.delete('tab')
        else out.set('tab', next)
        return out
      },
      { replace: true },
    )
  }

  return (
    <Tabs value={tab} onValueChange={change}>
      <TabsList aria-label="Secciones del host">
        <TabsTrigger value="resumen">Resumen</TabsTrigger>
        <TabsTrigger value="metricas">Métricas</TabsTrigger>
        <TabsTrigger value="chequeos">Chequeos</TabsTrigger>
        {visible.includes('parches') && <TabsTrigger value="parches">Parches</TabsTrigger>}
        {visible.includes('inventario') && <TabsTrigger value="inventario">Inventario</TabsTrigger>}
        <TabsTrigger value="historial">Historial de estado</TabsTrigger>
      </TabsList>
      <TabsContent value="resumen">{tab === 'resumen' && summary}</TabsContent>
      <TabsContent value="metricas">{tab === 'metricas' && metrics}</TabsContent>
      <TabsContent value="chequeos">
        {tab === 'chequeos' && (
          <div className="flex flex-col gap-3">
            <HostChecksSection hostId={hostId} title={false} />
            <CollectorDevices hostId={hostId} />
          </div>
        )}
      </TabsContent>
      <TabsContent value="parches">{tab === 'parches' && <HostUpdatesSection hostId={hostId} />}</TabsContent>
      <TabsContent value="inventario">{tab === 'inventario' && <InventoryContent hostId={hostId} onDenied={() => change('resumen')} />}</TabsContent>
      <TabsContent value="historial">{tab === 'historial' && <HostEventsSection hostId={hostId} />}</TabsContent>
    </Tabs>
  )
}
