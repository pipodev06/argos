import { Link } from 'react-router'
import { Button } from '@/components/ui/button'
import { useAuth } from '@/features/auth/hooks/useAuth'
import { canViewInventory, inventoryPath } from '../utils/inventory'

/** Boton al inventario del host (solo admin o `can_inventory`); los conteos viven en la propia pagina de inventario. */
export function HostInventorySummary({ hostId }: { hostId: string }) {
  const { user } = useAuth()
  if (!canViewInventory(user)) return null
  return (
    <Button asChild variant="outline" size="sm" className="w-fit">
      <Link to={inventoryPath(hostId)}>Ver inventario</Link>
    </Button>
  )
}
