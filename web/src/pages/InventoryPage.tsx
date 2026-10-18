import { useParams } from 'react-router'
import { InlineError } from '@/components/ui/inline-error'
import { useAuth } from '@/features/auth/hooks/useAuth'
import { BackLink } from '@/components/ui/back-link'
import { isHostId } from '@/features/hosts/utils/id'
import { InventoryView } from '@/features/inventory/components/InventoryView'
import { canViewInventory } from '@/features/inventory/utils/inventory'

export function InventoryPage() {
  const { id } = useParams()
  const { user } = useAuth()

  if (!id || !isHostId(id)) {
    return (
      <div className="flex flex-col gap-3">
        <BackLink to="/hosts">Hosts</BackLink>
        <InlineError>Id de host inválido.</InlineError>
      </div>
    )
  }
  if (!canViewInventory(user)) {
    return (
      <div className="flex flex-col gap-3">
        <BackLink to={`/hosts/${encodeURIComponent(id)}`}>Volver al host</BackLink>
        <InlineError>Inventario no disponible.</InlineError>
      </div>
    )
  }
  return <InventoryView key={id} id={id} />
}
