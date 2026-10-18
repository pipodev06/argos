import { TabsNav, TabsNavLink } from '@/components/ui/tabs'
import { useAuth } from '@/features/auth/hooks/useAuth'
import { hostTabPath, visibleHostTabs } from '../utils/tabs'

/** Barra de navegación del host: cada sección es una sub-ruta (`/hosts/:id/metricas`…), no un estado local. */
export function HostNav({ hostId, isCollector }: { hostId: string; isCollector: boolean }) {
  const { user } = useAuth()
  return (
    <TabsNav aria-label="Secciones del host">
      {visibleHostTabs(user, isCollector).map((t) => (
        <TabsNavLink key={t.id} to={hostTabPath(hostId, t.path)} end replace>
          {t.label}
        </TabsNavLink>
      ))}
    </TabsNav>
  )
}
