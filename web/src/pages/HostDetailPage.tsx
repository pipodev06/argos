import { useParams } from 'react-router'
import { InlineError } from '@/components/ui/inline-error'
import { BackLink } from '@/components/ui/back-link'
import { HostLayout } from '@/features/hosts/components/HostLayout'
import { isHostId } from '@/features/hosts/utils/id'

/** Layout de `/hosts/:id`: sus sub-rutas se declaran en DashboardPage. */
export function HostDetailPage() {
  const { id } = useParams()

  if (!id || !isHostId(id)) {
    return (
      <div className="flex flex-col gap-3">
        <BackLink to="/hosts">Hosts</BackLink>
        <InlineError>Id de host inválido.</InlineError>
      </div>
    )
  }
  // key: al cambiar de host se reinicia el estado del detalle.
  return <HostLayout key={id} id={id} />
}
