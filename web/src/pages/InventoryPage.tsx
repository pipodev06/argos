import { useParams } from 'react-router'
import { InlineError } from '@/components/ui/inline-error'
import { useAuth } from '@/features/auth/hooks/useAuth'
import { BackToHostsLink } from '@/features/hosts/components/BackToHostsLink'
import { isHostId } from '@/features/hosts/utils/id'
import { InventoryView } from '@/features/inventory/components/InventoryView'
import { canViewInventory } from '@/features/inventory/utils/inventory'

export function InventoryPage() {
  const { id } = useParams()
  const { user } = useAuth()

  if (!id || !isHostId(id)) {
    return (
      <div className="flex flex-col gap-6">
        <BackToHostsLink />
        <InlineError>Id de host inválido.</InlineError>
      </div>
    )
  }
  if (!canViewInventory(user)) {
    return (
      <div className="flex flex-col gap-6">
        <BackToHostsLink to={`/hosts/${encodeURIComponent(id)}`}>Volver al host</BackToHostsLink>
        <InlineError>Inventario no disponible.</InlineError>
      </div>
    )
  }
  return <InventoryView key={id} id={id} />
}
